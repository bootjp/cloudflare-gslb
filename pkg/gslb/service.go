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
	cf "github.com/cloudflare/cloudflare-go"
	"github.com/cockroachdb/errors"
)

var (
	ErrNoFailoverIPs         = errors.New("no failover IPs configured")
	ErrInvalidIPAddress      = errors.New("invalid IP address")
	ErrInvalidIPv4Address    = errors.New("not a valid IPv4 address for A record")
	ErrInvalidIPv6Address    = errors.New("not a valid IPv6 address for AAAA record")
	ErrUnsupportedRecordType = errors.New("unsupported record type")
)

type OriginStatus struct {
	CurrentIP       string
	UsingPriority   bool
	HealthyPriority bool
	LastCheck       time.Time
}

type Service struct {
	config          *config.Config
	dnsClient       cloudflare.DNSClientInterface
	checkMutex      sync.Mutex
	stopCh          chan struct{}
	wg              sync.WaitGroup
	failoverIndices map[string]int
	dnsClients      map[string]cloudflare.DNSClientInterface
	originStatus    map[string]*OriginStatus
	zoneMap         map[string]string
	zoneIDMap       map[string]string
}

func NewService(cfg *config.Config) (*Service, error) {
	var defaultClient cloudflare.DNSClientInterface
	if len(cfg.CloudflareZoneIDs) > 0 {
		client, err := cloudflare.NewDNSClient(
			cfg.CloudflareAPIToken,
			cfg.CloudflareZoneIDs[0].ZoneID,
			false,
			60,
		)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		defaultClient = client
	}

	zoneMap := make(map[string]string)
	zoneIDMap := make(map[string]string)

	for _, zone := range cfg.CloudflareZoneIDs {
		zoneMap[zone.ZoneID] = zone.Name
		zoneIDMap[zone.Name] = zone.ZoneID
	}

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

	return &Service{
		config:          cfg,
		dnsClient:       defaultClient,
		stopCh:          make(chan struct{}),
		failoverIndices: make(map[string]int),
		dnsClients:      dnsClients,
		originStatus:    make(map[string]*OriginStatus),
		zoneMap:         zoneMap,
		zoneIDMap:       zoneIDMap,
	}, nil
}

func (s *Service) getDNSClientForOrigin(origin config.OriginConfig) cloudflare.DNSClientInterface {
	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)
	client, exists := s.dnsClients[originKey]
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

	if _, exists := s.originStatus[originKey]; !exists {
		initialUsingPriority := len(origin.PriorityFailoverIPs) > 0
		log.Printf("Initializing state for %s: initialUsingPriority=%t (will be verified on first check)",
			origin.Name, initialUsingPriority)

		s.originStatus[originKey] = &OriginStatus{
			UsingPriority:   initialUsingPriority,
			HealthyPriority: true,
		}
	}

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
	status := s.originStatus[originKey]

	log.Printf("Checking priority IPs for %s, current status: UsingPriority=%t, HealthyPriority=%t, CurrentIP=%s",
		origin.Name, status.UsingPriority, status.HealthyPriority, status.CurrentIP)

	isPriorityIP := false
	for _, priorityIP := range origin.PriorityFailoverIPs {
		if status.CurrentIP == priorityIP {
			isPriorityIP = true
			break
		}
	}

	if isPriorityIP != status.UsingPriority {
		log.Printf("Fixing inconsistent state for %s: UsingPriority=%t but current IP %s is %s a priority IP",
			origin.Name, status.UsingPriority, status.CurrentIP,
			map[bool]string{true: "actually", false: "not"}[isPriorityIP])
		status.UsingPriority = isPriorityIP
	}

	if status.UsingPriority {
		log.Printf("Already using priority IP for %s, skipping check", origin.Name)
		return
	}

	allHealthy := true
	for _, ip := range origin.PriorityFailoverIPs {
		if err := checker.Check(ip); err != nil {
			log.Printf("Priority IP %s is still unhealthy: %v", ip, err)
			allHealthy = false
			break
		}
		log.Printf("Priority IP %s is healthy", ip)
	}

	if allHealthy {
		log.Printf("Priority IPs for %s are now healthy, switching back", origin.Name)
		status.HealthyPriority = true

		// 優先IPに戻すためのDNSレコード更新
		dnsClient := s.getDNSClientForOrigin(origin)
		priorityIP := origin.PriorityFailoverIPs[0]

		if err := dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, priorityIP); err != nil {
			log.Printf("Failed to switch back to priority IP for %s: %v", origin.Name, err)
			return
		}

		// 状態を更新
		status.CurrentIP = priorityIP
		status.UsingPriority = true
		log.Printf("Successfully switched back to priority IP %s for %s", priorityIP, origin.Name)
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
	status, exists := s.originStatus[originKey]
	if !exists {
		status = &OriginStatus{
			UsingPriority:   false,
			HealthyPriority: true,
		}
		s.originStatus[originKey] = status
	}
	return status
}

func (s *Service) processRecord(ctx context.Context, origin config.OriginConfig, record cf.DNSRecord, checker healthcheck.Checker, status *OriginStatus) {
	ip := record.Content
	status.CurrentIP = ip

	err := checker.Check(ip)
	if err != nil {
		log.Printf("Health check failed for %s (%s): %v", origin.Name, ip, err)

		if status.UsingPriority && len(origin.PriorityFailoverIPs) > 0 {
			status.HealthyPriority = false
			status.UsingPriority = false
		}

		if err := s.replaceUnhealthyRecord(ctx, origin, record); err != nil {
			log.Printf("Failed to replace unhealthy record for %s: %v", origin.Name, err)
		}
	} else {
		log.Printf("Health check passed for %s (%s)", origin.Name, ip)

		isPriorityIP := false
		for _, priorityIP := range origin.PriorityFailoverIPs {
			if ip == priorityIP {
				isPriorityIP = true
				break
			}
		}

		status.UsingPriority = isPriorityIP
		status.CurrentIP = ip
		status.LastCheck = time.Now()
	}
}

func (s *Service) replaceUnhealthyRecord(ctx context.Context, origin config.OriginConfig, unhealthyRecord cf.DNSRecord) error {
	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)

	dnsClient := s.getDNSClientForOrigin(origin)

	status := s.originStatus[originKey]

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
	status.UsingPriority = false

	newIP := origin.FailoverIPs[0]
	s.failoverIndices[originKey] = 0

	log.Printf("Switching from priority IP to regular failover IP: %s for %s",
		newIP, origin.Name)
	return dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP)
}

func (s *Service) useNextFailoverIP(ctx context.Context, origin config.OriginConfig, unhealthyRecord cf.DNSRecord, dnsClient cloudflare.DNSClientInterface, originKey string) error {
	currentIndex, exists := s.failoverIndices[originKey]
	if !exists {
		currentIndex = 0
	}

	nextIndex := (currentIndex + 1) % len(origin.FailoverIPs)
	s.failoverIndices[originKey] = nextIndex

	newIP := origin.FailoverIPs[nextIndex]

	if err := s.validateIPType(origin.RecordType, newIP); err != nil {
		return err
	}

	log.Printf("Replacing unhealthy record %s with failover IP: %s (index: %d, proxied: %t)",
		unhealthyRecord.Content, newIP, nextIndex, origin.Proxied)
	return dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP)
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
		if multiErr == nil {
			multiErr = err
		} else {
			multiErr = fmt.Errorf("%w; %w", multiErr, err)
		}
	}

	if multiErr != nil {
		return multiErr
	}

	log.Println("One-shot health check completed")
	return nil
}
