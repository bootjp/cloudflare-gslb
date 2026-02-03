package gslb

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
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
	ErrInvalidIPAddress       = errors.New("invalid IP address")
	ErrInvalidIPv4Address     = errors.New("not a valid IPv4 address for A record")
	ErrInvalidIPv6Address     = errors.New("not a valid IPv6 address for AAAA record")
	ErrUnsupportedRecordType  = errors.New("unsupported record type")
	ErrNoCloudflareZoneConfig = errors.New("no cloudflare zone configured")
)

type OriginStatus struct {
	CurrentPriority int
	CurrentIPs      []string
	Initialized     bool
	LastCheck       time.Time
}

type Service struct {
	config     *config.Config
	dnsClient  cloudflare.DNSClientInterface
	checkMutex sync.Mutex
	stopCh     chan struct{}
	wg         sync.WaitGroup

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
		config:       cfg,
		dnsClient:    defaultClient,
		stopCh:       make(chan struct{}),
		dnsClients:   dnsClients,
		originStatus: make(map[string]*OriginStatus),
		zoneMap:      zoneMap,
		zoneIDMap:    zoneIDMap,
		notifiers:    notifiers,
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
		s.originStatus[originKey] = &OriginStatus{}
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
		}
	}
}

func (s *Service) checkOrigin(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) {
	s.checkMutex.Lock()
	defer s.checkMutex.Unlock()

	log.Printf("Checking origin: %s (%s)", origin.Name, origin.RecordType)

	priorityLevels := origin.EffectivePriorityLevels()
	if len(priorityLevels) == 0 {
		log.Printf("No priority levels configured for %s", origin.Name)
		return
	}
	priorityLevels = sortPriorityLevels(priorityLevels)
	maxPriority := priorityLevels[0].Priority

	dnsClient := s.getDNSClientForOrigin(origin)

	records, err := dnsClient.GetDNSRecords(ctx, origin.Name, origin.RecordType)
	if err != nil {
		log.Printf("Failed to get DNS records for %s: %v", origin.Name, err)
		return
	}

	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)
	status := s.getOrInitOriginStatus(originKey)

	currentIPs := collectRecordIPs(records)

	currentPriority := status.CurrentPriority
	currentPrioritySet := status.Initialized

	if detectedPriority, ok := detectCurrentPriority(priorityLevels, currentIPs); ok {
		currentPriority = detectedPriority
		currentPrioritySet = true
	}

	if !currentPrioritySet {
		currentPriority = maxPriority
		currentPrioritySet = true
	}

	selectedPriority, selectedIPs, ok := s.selectPriorityLevel(origin, checker, priorityLevels, currentPriority, currentPrioritySet)
	if !ok {
		log.Printf("No healthy IPs available for %s", origin.Name)
		s.updateOriginStatus(originKey, currentPriority, currentIPs, currentPrioritySet)
		return
	}

	selectedIPs = s.filterValidIPs(origin.RecordType, selectedIPs)
	if len(selectedIPs) == 0 {
		log.Printf("No valid IPs available for %s (%s)", origin.Name, origin.RecordType)
		s.updateOriginStatus(originKey, currentPriority, currentIPs, currentPrioritySet)
		return
	}

	if sameIPSet(currentIPs, selectedIPs) {
		s.updateOriginStatus(originKey, selectedPriority, selectedIPs, true)
		return
	}

	if err := dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, selectedIPs); err != nil {
		log.Printf("Failed to update DNS records for %s: %v", origin.Name, err)
		return
	}

	s.updateOriginStatus(originKey, selectedPriority, selectedIPs, true)

	isPriorityIP := selectedPriority == maxPriority
	isFailoverIP := selectedPriority < maxPriority
	reason := buildChangeReason(currentPrioritySet, currentPriority, selectedPriority, currentIPs, selectedIPs)

	s.sendNotifications(origin, currentIPs, selectedIPs, reason, isPriorityIP, isFailoverIP, currentPriority, selectedPriority, maxPriority)
}

func (s *Service) getOrInitOriginStatus(originKey string) *OriginStatus {
	s.originStatusMutex.RLock()
	status, exists := s.originStatus[originKey]
	s.originStatusMutex.RUnlock()

	if !exists {
		status = &OriginStatus{}
		s.originStatusMutex.Lock()
		s.originStatus[originKey] = status
		s.originStatusMutex.Unlock()
	}
	return status
}

func (s *Service) selectPriorityLevel(origin config.OriginConfig, checker healthcheck.Checker, levels []config.PriorityLevel, currentPriority int, currentPrioritySet bool) (int, []string, bool) {
	if !origin.ReturnToPriority && currentPrioritySet {
		if level, ok := findPriorityLevel(levels, currentPriority); ok {
			if s.checkPriorityLevel(origin.RecordType, checker, level) {
				return currentPriority, level.IPs, true
			}
		}
	}

	for _, level := range levels {
		if !origin.ReturnToPriority && currentPrioritySet && level.Priority > currentPriority {
			continue
		}

		if s.checkPriorityLevel(origin.RecordType, checker, level) {
			return level.Priority, level.IPs, true
		}
	}

	return 0, nil, false
}

func (s *Service) checkPriorityLevel(recordType string, checker healthcheck.Checker, level config.PriorityLevel) bool {
	log.Printf("Checking priority level %d (%d IPs)", level.Priority, len(level.IPs))

	if len(level.IPs) == 0 {
		return false
	}

	for _, ip := range level.IPs {
		if err := s.validateIPType(recordType, ip); err != nil {
			log.Printf("Invalid IP %s for record type %s: %v", ip, recordType, err)
			return false
		}
		if err := checker.Check(ip); err != nil {
			log.Printf("IP %s at priority %d is unhealthy: %v", ip, level.Priority, err)
			return false
		}
	}

	return true
}

func (s *Service) filterValidIPs(recordType string, ips []string) []string {
	valid := make([]string, 0, len(ips))
	for _, ip := range ips {
		if err := s.validateIPType(recordType, ip); err != nil {
			log.Printf("Invalid IP %s for record type %s: %v", ip, recordType, err)
			continue
		}
		valid = append(valid, ip)
	}
	return valid
}

func (s *Service) updateOriginStatus(originKey string, priority int, ips []string, initialized bool) {
	s.originStatusMutex.Lock()
	defer s.originStatusMutex.Unlock()

	status := s.originStatus[originKey]
	if status == nil {
		status = &OriginStatus{}
		s.originStatus[originKey] = status
	}

	if initialized {
		status.CurrentPriority = priority
		status.Initialized = true
	}
	status.CurrentIPs = ips
	status.LastCheck = time.Now()
}

func sortPriorityLevels(levels []config.PriorityLevel) []config.PriorityLevel {
	sorted := make([]config.PriorityLevel, len(levels))
	copy(sorted, levels)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	return sorted
}

func findPriorityLevel(levels []config.PriorityLevel, priority int) (config.PriorityLevel, bool) {
	for _, level := range levels {
		if level.Priority == priority {
			return level, true
		}
	}
	return config.PriorityLevel{}, false
}

func detectCurrentPriority(levels []config.PriorityLevel, currentIPs []string) (int, bool) {
	if len(levels) == 0 || len(currentIPs) == 0 {
		return 0, false
	}

	currentSet := sliceToSet(currentIPs)
	matchedPriority := findHighestMatchingPriority(levels, currentSet)
	if matchedPriority == nil {
		return 0, false
	}
	return *matchedPriority, true
}

func findHighestMatchingPriority(levels []config.PriorityLevel, currentSet map[string]struct{}) *int {
	var matched *int
	for _, level := range levels {
		if !isSubset(currentSet, sliceToSet(level.IPs)) {
			continue
		}
		matched = pickHigherPriority(matched, level.Priority)
	}
	return matched
}

func sliceToSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func isSubset(subset map[string]struct{}, superset map[string]struct{}) bool {
	for value := range subset {
		if _, ok := superset[value]; !ok {
			return false
		}
	}
	return true
}

func pickHigherPriority(current *int, candidate int) *int {
	if current == nil || candidate > *current {
		next := candidate
		return &next
	}
	return current
}

func collectRecordIPs(records []dns.RecordResponse) []string {
	ips := make([]string, 0, len(records))
	for _, record := range records {
		if record.Content != "" {
			ips = append(ips, record.Content)
		}
	}
	return ips
}

func sameIPSet(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	setA := make(map[string]struct{}, len(a))
	for _, ip := range a {
		setA[ip] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, ip := range b {
		setB[ip] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for ip := range setA {
		if _, ok := setB[ip]; !ok {
			return false
		}
	}
	return true
}

func buildChangeReason(currentPrioritySet bool, currentPriority, selectedPriority int, currentIPs, selectedIPs []string) string {
	if !currentPrioritySet {
		return fmt.Sprintf("Switching to priority level %d", selectedPriority)
	}
	if selectedPriority > currentPriority {
		return fmt.Sprintf("Priority level %d is healthy again", selectedPriority)
	}
	if selectedPriority < currentPriority {
		return fmt.Sprintf("Priority level %d unhealthy, switching to level %d", currentPriority, selectedPriority)
	}
	if !sameIPSet(currentIPs, selectedIPs) {
		return fmt.Sprintf("Updating IPs within priority level %d based on health checks", selectedPriority)
	}
	return "No change"
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

func (s *Service) sendNotifications(origin config.OriginConfig, oldIPs, newIPs []string, reason string, isPriorityIP, isFailoverIP bool, oldPriority, newPriority, maxPriority int) {
	if len(s.notifiers) == 0 {
		return
	}

	event := notifier.FailoverEvent{
		OriginName:       origin.Name,
		ZoneName:         origin.ZoneName,
		RecordType:       origin.RecordType,
		OldIP:            firstIP(oldIPs),
		NewIP:            firstIP(newIPs),
		OldIPs:           oldIPs,
		NewIPs:           newIPs,
		Reason:           reason,
		Timestamp:        time.Now(),
		IsPriorityIP:     isPriorityIP,
		IsFailoverIP:     isFailoverIP,
		ReturnToPriority: origin.ReturnToPriority,
		OldPriority:      oldPriority,
		NewPriority:      newPriority,
		MaxPriority:      maxPriority,
	}

	// Create a context with timeout for notifications independent of parent cancellation
	// Important: Do not cancel immediately on function return since notifications are sent in goroutines
	notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	var wg sync.WaitGroup
	for _, n := range s.notifiers {
		wg.Add(1)
		go func(notifier notifier.Notifier) {
			defer wg.Done()
			if err := notifier.Notify(notifyCtx, event); err != nil {
				log.Printf("Failed to send notification: %v", err)
			} else {
				log.Printf("Notification sent successfully for %s.%s (%v -> %v)",
					origin.Name, origin.ZoneName, oldIPs, newIPs)
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

func firstIP(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

func (s *Service) runOriginCheck(ctx context.Context, origin config.OriginConfig) error {
	checker, err := healthcheck.NewChecker(origin.HealthCheck)
	if err != nil {
		return fmt.Errorf("failed to create health checker for %s: %w", origin.Name, err)
	}
	s.checkOrigin(ctx, origin, checker)
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
