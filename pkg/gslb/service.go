package gslb

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	"github.com/bootjp/cloudflare-gslb/pkg/healthcheck"
	"github.com/bootjp/cloudflare-gslb/pkg/notifier"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cockroachdb/errors"
)

var (
	ErrNoFailoverIPs          = errors.New("no failover IPs configured")
	ErrInvalidIPAddress       = errors.New("invalid IP address")
	ErrInvalidIPv4Address     = errors.New("not a valid IPv4 address for A record")
	ErrInvalidIPv6Address     = errors.New("not a valid IPv6 address for AAAA record")
	ErrUnsupportedRecordType  = errors.New("unsupported record type")
	ErrNoCloudflareZoneConfig = errors.New("no cloudflare zone configured")
)

type OriginStatus struct {
	CurrentIP       string
	UsingPriority   bool
	HealthyPriority bool
	LastCheck       time.Time
}

type Service struct {
	config     *config.Config
	dnsClient  cloudflare.DNSClientInterface
	checkMutex sync.Mutex
	stopCh     chan struct{}
	wg         sync.WaitGroup

	failoverMutex   sync.RWMutex
	failoverIndices map[string]int

	dnsClientsMutex sync.RWMutex
	dnsClients      map[string]cloudflare.DNSClientInterface

	originStatusMutex sync.RWMutex
	originStatus      map[string]*OriginStatus

	zoneMap   map[string]string
	zoneIDMap map[string]string

	notifiers []notifier.Notifier
}

func buildZoneMaps(cfg *config.Config) (map[string]string, map[string]string) {
	zoneMap := make(map[string]string)
	zoneIDMap := make(map[string]string)

	for _, zone := range cfg.CloudflareZoneIDs {
		zoneMap[zone.ZoneID] = zone.Name
		zoneIDMap[zone.Name] = zone.ZoneID
	}

	return zoneMap, zoneIDMap
}

func buildDNSClients(cfg *config.Config, zoneIDMap map[string]string) (map[string]cloudflare.DNSClientInterface, error) {
	dnsClients := make(map[string]cloudflare.DNSClientInterface)

	for _, origin := range cfg.Origins {
		zoneID, exists := zoneIDMap[origin.ZoneName]
		if !exists {
			return nil, errors.Newf("zone name %s not found in configuration", origin.ZoneName)
		}

		originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

		client, err := cloudflare.NewDNSClient(
			cfg.CloudflareAPIToken,
			zoneID,
			origin.Proxied,
			60,
		)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		dnsClients[originKey] = client
	}

	return dnsClients, nil
}

func buildNotifiers(cfg *config.Config) []notifier.Notifier {
	notifiers := make([]notifier.Notifier, 0)
	for _, nc := range cfg.Notifications {
		switch nc.Type {
		case "slack":
			notifiers = append(notifiers, notifier.NewSlackNotifier(nc.WebhookURL))
			log.Printf("Slack notifier configured")
		case "discord":
			notifiers = append(notifiers, notifier.NewDiscordNotifier(nc.WebhookURL))
			log.Printf("Discord notifier configured")
		default:
			log.Printf("Unknown notification type: %s", nc.Type)
		}
	}
	return notifiers
}

func NewService(cfg *config.Config) (*Service, error) {
	if len(cfg.CloudflareZoneIDs) == 0 {
		return nil, ErrNoCloudflareZoneConfig
	}

	defaultClient, err := cloudflare.NewDNSClient(
		cfg.CloudflareAPIToken,
		cfg.CloudflareZoneIDs[0].ZoneID,
		false,
		60,
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	zoneMap, zoneIDMap := buildZoneMaps(cfg)

	dnsClients, err := buildDNSClients(cfg, zoneIDMap)
	if err != nil {
		return nil, err
	}

	notifiers := buildNotifiers(cfg)

	return &Service{
		config:          cfg,
		dnsClient:       defaultClient,
		stopCh:          make(chan struct{}),
		failoverIndices: make(map[string]int),
		dnsClients:      dnsClients,
		originStatus:    make(map[string]*OriginStatus),
		zoneMap:         zoneMap,
		zoneIDMap:       zoneIDMap,
		notifiers:       notifiers,
	}, nil
}

func (s *Service) getDNSClientForOrigin(origin config.OriginConfig) cloudflare.DNSClientInterface {
	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

	s.dnsClientsMutex.RLock()
	client, exists := s.dnsClients[originKey]
	s.dnsClientsMutex.RUnlock()

	if !exists {
		return s.dnsClient
	}
	return client
}

func (s *Service) Start(ctx context.Context) error {
	log.Println("Starting GSLB service...")

	for _, origin := range s.config.Origins {
		s.wg.Add(1)
		go s.monitorOrigin(ctx, origin)
	}

	return nil
}

func (s *Service) Stop() {
	log.Println("Stopping GSLB service...")
	close(s.stopCh)
	s.wg.Wait()
	log.Println("GSLB service stopped")
}

func (s *Service) monitorOrigin(ctx context.Context, origin config.OriginConfig) {
	defer s.wg.Done()

	log.Printf("Starting monitoring for origin: %s (%s)", origin.Name, origin.RecordType)

	checker, err := healthcheck.NewChecker(origin.HealthCheck)
	if err != nil {
		log.Printf("Failed to create health checker for %s: %v", origin.Name, err)
		return
	}

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

	s.originStatusMutex.Lock()
	if _, exists := s.originStatus[originKey]; !exists {
		initialUsingPriority := len(origin.PriorityFailoverIPs) > 0
		log.Printf("Initializing state for %s: initialUsingPriority=%t (will be verified on first check)",
			origin.Name, initialUsingPriority)

		s.originStatus[originKey] = &OriginStatus{
			UsingPriority:   initialUsingPriority,
			HealthyPriority: true,
		}
	}
	s.originStatusMutex.Unlock()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Printf("Running check cycle for origin: %s (%s)", origin.Name, origin.RecordType)
			s.checkOrigin(ctx, origin, checker)
			if origin.ReturnToPriority && len(origin.PriorityFailoverIPs) > 0 {
				log.Printf("ReturnToPriority is enabled, checking priority IPs for %s", origin.Name)
				s.checkPriorityIPs(ctx, origin, checker)
			} else {
				log.Printf("ReturnToPriority is disabled or no priority IPs for %s", origin.Name)
			}
		}
	}
}

func (s *Service) checkPriorityIPs(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) {
	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

	s.originStatusMutex.RLock()
	status := s.originStatus[originKey]
	s.originStatusMutex.RUnlock()

	log.Printf("Checking priority IPs for %s, current status: UsingPriority=%t, HealthyPriority=%t, CurrentIP=%s",
		origin.Name, status.UsingPriority, status.HealthyPriority, status.CurrentIP)

	isPriorityIP := origin.IsPriorityIP(status.CurrentIP)

	if isPriorityIP != status.UsingPriority {
		log.Printf("Fixing inconsistent state for %s: UsingPriority=%t but current IP %s is %s a priority IP",
			origin.Name, status.UsingPriority, status.CurrentIP,
			map[bool]string{true: "actually", false: "not"}[isPriorityIP])

		s.originStatusMutex.Lock()
		status.UsingPriority = isPriorityIP
		s.originStatusMutex.Unlock()
	}

	if status.UsingPriority {
		log.Printf("Already using priority IP for %s, skipping check", origin.Name)
		return
	}

	// 優先度順にソートされたIPリストを取得
	priorityIPs := origin.GetPriorityIPs()

	// 最も優先度の高い健全なIPを見つける（同一優先度のIPはすべてチェック）
	var healthyPriorityIPs []string
	const noPriorityFound = -1
	foundPriority := noPriorityFound

	// IP から優先度を即時参照できるようマップを作成（ループごとの線形探索を回避）
	ipToPriority := make(map[string]int, len(origin.PriorityFailoverIPs))
	for _, p := range origin.PriorityFailoverIPs {
		ipToPriority[p.IP] = p.Priority
	}

	for _, ip := range priorityIPs {
		// このIPの優先度を取得（存在しない場合は 0：元のコードと同様のゼロ値）
		currentPriority := ipToPriority[ip]

		// すでに健全なIPが見つかっている場合、異なる優先度のIPはスキップ
		if foundPriority != noPriorityFound && currentPriority != foundPriority {
			break
		}

		if err := checker.Check(ip); err != nil {
			log.Printf("Priority IP %s is still unhealthy: %v", ip, err)
			continue
		}
		log.Printf("Priority IP %s is healthy", ip)
		healthyPriorityIPs = append(healthyPriorityIPs, ip)
		foundPriority = currentPriority
	}

	if len(healthyPriorityIPs) > 0 {
		log.Printf("Found %d healthy priority IP(s) for %s: %v, switching back", len(healthyPriorityIPs), origin.Name, healthyPriorityIPs)

		s.originStatusMutex.Lock()
		oldIP := status.CurrentIP
		s.originStatusMutex.Unlock()

		// 優先IPに戻すためのDNSレコード更新
		dnsClient := s.getDNSClientForOrigin(origin)

		if err := dnsClient.ReplaceRecordsMultiple(ctx, origin.Name, origin.RecordType, healthyPriorityIPs); err != nil {
			log.Printf("Failed to switch back to priority IP(s) for %s: %v", origin.Name, err)
			return
		}

		// DNS更新が成功した場合のみ状態を更新（最初のIPを代表として保存）
		s.originStatusMutex.Lock()
		status.HealthyPriority = true
		status.CurrentIP = healthyPriorityIPs[0]
		status.UsingPriority = true
		s.originStatusMutex.Unlock()

		newIPsStr := fmt.Sprintf("%v", healthyPriorityIPs)
		log.Printf("Successfully switched back to priority IP(s) %s for %s", newIPsStr, origin.Name)

		// 通知を送信（すべての優先IPを含める）
		s.sendNotificationsMultiple(ctx, origin, oldIP, healthyPriorityIPs, "Priority IP is healthy again", true, false)
	}
}

func (s *Service) checkOrigin(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) {
	s.checkMutex.Lock()
	defer s.checkMutex.Unlock()

	log.Printf("Checking origin: %s (%s)", origin.Name, origin.RecordType)

	dnsClient := s.getDNSClientForOrigin(origin)

	records, err := dnsClient.GetDNSRecords(ctx, origin.Name, origin.RecordType)
	if err != nil {
		log.Printf("Failed to get DNS records for %s: %v", origin.Name, err)
		return
	}

	if len(records) == 0 {
		log.Printf("No DNS records found for %s", origin.Name)
		return
	}

	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)
	status := s.getOrInitOriginStatus(originKey)

	for _, record := range records {
		s.processRecord(ctx, origin, record, checker, status)
	}
}

func (s *Service) getOrInitOriginStatus(originKey string) *OriginStatus {
	s.originStatusMutex.RLock()
	status, exists := s.originStatus[originKey]
	s.originStatusMutex.RUnlock()

	if !exists {
		status = &OriginStatus{
			UsingPriority:   false,
			HealthyPriority: true,
		}
		s.originStatusMutex.Lock()
		s.originStatus[originKey] = status
		s.originStatusMutex.Unlock()
	}
	return status
}

func (s *Service) processRecord(ctx context.Context, origin config.OriginConfig, record dns.RecordResponse, checker healthcheck.Checker, status *OriginStatus) {
	ip := record.Content

	// OriginStatusの更新にはロックが必要
	s.originStatusMutex.Lock()
	status.CurrentIP = ip
	s.originStatusMutex.Unlock()

	err := checker.Check(ip)
	if err != nil {
		log.Printf("Health check failed for %s (%s): %v", origin.Name, ip, err)

		s.originStatusMutex.Lock()
		if status.UsingPriority && len(origin.PriorityFailoverIPs) > 0 {
			status.HealthyPriority = false
			status.UsingPriority = false
		}
		s.originStatusMutex.Unlock()

		if err := s.replaceUnhealthyRecord(ctx, origin, record); err != nil {
			log.Printf("Failed to replace unhealthy record for %s: %v", origin.Name, err)
		}
	} else {
		log.Printf("Health check passed for %s (%s)", origin.Name, ip)

		isPriorityIP := origin.IsPriorityIP(ip)

		s.originStatusMutex.Lock()
		status.UsingPriority = isPriorityIP
		status.CurrentIP = ip
		status.LastCheck = time.Now()
		s.originStatusMutex.Unlock()
	}
}

func (s *Service) replaceUnhealthyRecord(ctx context.Context, origin config.OriginConfig, unhealthyRecord dns.RecordResponse) error {
	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

	dnsClient := s.getDNSClientForOrigin(origin)

	s.originStatusMutex.RLock()
	status := s.originStatus[originKey]
	s.originStatusMutex.RUnlock()

	if status.UsingPriority && !status.HealthyPriority && len(origin.FailoverIPs) > 0 {
		return s.switchToPrimaryFailover(ctx, origin, dnsClient, originKey, status)
	}

	if len(origin.FailoverIPs) > 0 {
		if err := s.validateIPType(origin.RecordType, unhealthyRecord.Content); err != nil {
			return err
		}

		return s.useNextFailoverIP(ctx, origin, unhealthyRecord, dnsClient, originKey)
	}

	return errors.WithStack(ErrNoFailoverIPs)
}

func (s *Service) switchToPrimaryFailover(ctx context.Context, origin config.OriginConfig, dnsClient cloudflare.DNSClientInterface, originKey string, status *OriginStatus) error {
	s.originStatusMutex.Lock()
	oldIP := status.CurrentIP
	status.UsingPriority = false
	s.originStatusMutex.Unlock()

	newIP := origin.FailoverIPs[0]

	if err := s.validateIPType(origin.RecordType, newIP); err != nil {
		return err
	}

	s.failoverMutex.Lock()
	s.failoverIndices[originKey] = 0
	s.failoverMutex.Unlock()

	log.Printf("Switching from priority IP to regular failover IP: %s for %s",
		newIP, origin.Name)

	if err := dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP); err != nil {
		return err
	}

	// 通知を送信
	s.sendNotifications(ctx, origin, oldIP, newIP, "Priority IP failed, switching to backup IP", false, true)
	return nil
}

func (s *Service) useNextFailoverIP(ctx context.Context, origin config.OriginConfig, unhealthyRecord dns.RecordResponse, dnsClient cloudflare.DNSClientInterface, originKey string) error {
	s.failoverMutex.RLock()
	currentIndex, exists := s.failoverIndices[originKey]
	s.failoverMutex.RUnlock()

	if !exists {
		currentIndex = 0
	}

	nextIndex := (currentIndex + 1) % len(origin.FailoverIPs)

	s.failoverMutex.Lock()
	s.failoverIndices[originKey] = nextIndex
	s.failoverMutex.Unlock()

	newIP := origin.FailoverIPs[nextIndex]

	if err := s.validateIPType(origin.RecordType, newIP); err != nil {
		return err
	}

	oldIP := unhealthyRecord.Content
	log.Printf("Replacing unhealthy record %s with failover IP: %s (index: %d, proxied: %t)",
		oldIP, newIP, nextIndex, origin.Proxied)

	if err := dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP); err != nil {
		return err
	}

	// 通知を送信
	s.sendNotifications(ctx, origin, oldIP, newIP, "Health check failed, switching to backup IP", false, true)
	return nil
}

func (s *Service) validateIPType(recordType, ipAddress string) error {
	if recordType != "A" && recordType != "AAAA" {
		return errors.WithStack(ErrUnsupportedRecordType)
	}

	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return errors.WithStack(ErrInvalidIPAddress)
	}

	if recordType == "A" && ip.To4() == nil {
		return errors.WithStack(ErrInvalidIPv4Address)
	} else if recordType == "AAAA" && ip.To4() != nil {
		return errors.WithStack(ErrInvalidIPv6Address)
	}

	return nil
}

func (s *Service) sendNotifications(ctx context.Context, origin config.OriginConfig, oldIP, newIP, reason string, isPriorityIP, isFailoverIP bool) {
	s.sendNotificationsMultiple(ctx, origin, oldIP, []string{newIP}, reason, isPriorityIP, isFailoverIP)
}

func (s *Service) sendNotificationsMultiple(ctx context.Context, origin config.OriginConfig, oldIP string, newIPs []string, reason string, isPriorityIP, isFailoverIP bool) {
	if len(s.notifiers) == 0 {
		return
	}

	// Use first IP as primary for backward compatibility
	newIP := ""
	if len(newIPs) > 0 {
		newIP = newIPs[0]
	}

	event := notifier.FailoverEvent{
		OriginName:       origin.Name,
		ZoneName:         origin.ZoneName,
		RecordType:       origin.RecordType,
		OldIP:            oldIP,
		NewIP:            newIP,
		NewIPs:           newIPs,
		Reason:           reason,
		Timestamp:        time.Now(),
		IsPriorityIP:     isPriorityIP,
		IsFailoverIP:     isFailoverIP,
		ReturnToPriority: origin.ReturnToPriority,
	}

	// Create a context with timeout for notifications independent of parent cancellation
	// Important: Do not cancel immediately on function return since notifications are sent in goroutines
	notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	newIPsDisplay := event.GetNewIPsDisplay()

	var wg sync.WaitGroup
	for _, n := range s.notifiers {
		wg.Add(1)
		go func(notifier notifier.Notifier) {
			defer wg.Done()
			if err := notifier.Notify(notifyCtx, event); err != nil {
				log.Printf("Failed to send notification: %v", err)
			} else {
				log.Printf("Notification sent successfully for %s.%s (%s -> %s)",
					origin.Name, origin.ZoneName, oldIP, newIPsDisplay)
			}
		}(n)
	}

	// Wait for all notifications to complete in a separate goroutine to not block failover
	// Cancel the context after all notification goroutines have finished
	go func() {
		wg.Wait()
		cancel()
	}()
}

func (s *Service) runOriginCheck(ctx context.Context, origin config.OriginConfig) error {
	checker, err := healthcheck.NewChecker(origin.HealthCheck)
	if err != nil {
		return fmt.Errorf("failed to create health checker for %s: %w", origin.Name, err)
	}

	log.Printf("Checking origin: %s (%s)", origin.Name, origin.RecordType)

	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)
	status := s.getOrInitOriginStatus(originKey)

	dnsClient := s.getDNSClientForOrigin(origin)
	records, err := dnsClient.GetDNSRecords(ctx, origin.Name, origin.RecordType)
	if err != nil {
		return fmt.Errorf("failed to get DNS records for %s: %w", origin.Name, err)
	}

	if len(records) == 0 {
		log.Printf("No DNS records found for %s (%s)", origin.Name, origin.RecordType)
		return nil
	}

	for _, record := range records {
		s.processRecord(ctx, origin, record, checker, status)
	}

	if origin.ReturnToPriority && len(origin.PriorityFailoverIPs) > 0 {
		log.Printf("ReturnToPriority is enabled, checking priority IPs for %s", origin.Name)
		s.checkPriorityIPs(ctx, origin, checker)
	}

	return nil
}

func (s *Service) RunOneShot(ctx context.Context) error {
	log.Println("Running one-shot health check for all origins...")

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.config.Origins))

	for _, origin := range s.config.Origins {
		wg.Add(1)
		go func(o config.OriginConfig) {
			defer wg.Done()
			if err := s.runOriginCheck(ctx, o); err != nil {
				errCh <- err
			}
		}(origin)
	}

	wg.Wait()
	close(errCh)

	var multiErr error
	for err := range errCh {
		multiErr = errors.Join(multiErr, err)
	}

	if multiErr != nil {
		return multiErr
	}

	log.Println("One-shot health check completed")
	return nil
}
