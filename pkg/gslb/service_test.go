package gslb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	cfmock "github.com/bootjp/cloudflare-gslb/pkg/cloudflare/mock"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	"github.com/bootjp/cloudflare-gslb/pkg/notifier"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

type MockDNSClient struct {
	*cfmock.DNSClientMock
}

type notifierFunc func(context.Context, notifier.FailoverEvent) error

func (f notifierFunc) Notify(ctx context.Context, event notifier.FailoverEvent) error {
	return f(ctx, event)
}

func createTestService(origin config.OriginConfig) (*Service, *cfmock.DNSClientMock) {
	cfg := &config.Config{
		CloudflareAPIToken: "test-token",
		CloudflareZoneIDs: []config.ZoneConfig{
			{
				ZoneID: "test-zone",
				Name:   "default",
			},
		},
		CheckInterval: 1 * time.Second,
		Origins:       []config.OriginConfig{origin},
	}

	dnsClientMock := cfmock.NewDNSClientMock()
	mockClient := &MockDNSClient{dnsClientMock}

	service := &Service{
		config:       cfg,
		dnsClient:    mockClient,
		stopCh:       make(chan struct{}),
		dnsClients:   make(map[string]cloudflare.DNSClientInterface),
		originStatus: make(map[string]*OriginStatus),
		zoneMap:      map[string]string{"test-zone": "default"},
		zoneIDMap:    map[string]string{"default": "test-zone"},
	}

	originKey := fmt.Sprintf("%s-%s-%s", origin.ZoneName, origin.Name, origin.RecordType)
	service.dnsClients[originKey] = mockClient

	return service, dnsClientMock
}

func TestServiceCheckOrigin_SelectsHighestPriority(t *testing.T) {
	origin := config.OriginConfig{
		Name:       "example.com",
		ZoneName:   "default",
		RecordType: "A",
		HealthCheck: config.HealthCheck{
			Type:     "http",
			Endpoint: "/health",
			Timeout:  5,
		},
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.168.1.1", "192.168.1.2"}},
			{Priority: 50, IPs: []string{"192.168.1.3"}},
		},
		ReturnToPriority: true,
	}

	service, dnsClientMock := createTestService(origin)

	dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    dns.RecordResponseTypeA,
			Content: "192.168.1.3",
		}}, nil
	}

	replaceCallCount := 0
	var replaced []string
	dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType string, newContents []string) error {
		replaceCallCount++
		replaced = append([]string{}, newContents...)
		return nil
	}

	checker := hcmock.NewCheckerMock(func(ip string) error {
		return nil
	})

	service.checkOrigin(context.Background(), origin, checker)

	if replaceCallCount != 1 {
		t.Fatalf("ReplaceRecords was called %d times, expected 1", replaceCallCount)
	}
	if !sameStringSet(replaced, []string{"192.168.1.1", "192.168.1.2"}) {
		t.Fatalf("expected highest priority IPs, got %v", replaced)
	}
}

func TestServiceCheckOrigin_FallbackToLowerPriority(t *testing.T) {
	origin := config.OriginConfig{
		Name:       "example.com",
		ZoneName:   "default",
		RecordType: "A",
		HealthCheck: config.HealthCheck{
			Type:     "http",
			Endpoint: "/health",
			Timeout:  5,
		},
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.168.1.1", "192.168.1.2"}},
			{Priority: 50, IPs: []string{"192.168.1.3"}},
		},
		ReturnToPriority: true,
	}

	service, dnsClientMock := createTestService(origin)

	dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    dns.RecordResponseTypeA,
			Content: "192.168.1.1",
		}}, nil
	}

	replaceCallCount := 0
	var replaced []string
	dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType string, newContents []string) error {
		replaceCallCount++
		replaced = append([]string{}, newContents...)
		return nil
	}

	checker := hcmock.NewCheckerMock(func(ip string) error {
		if ip == "192.168.1.1" || ip == "192.168.1.2" {
			return fmt.Errorf("unhealthy")
		}
		return nil
	})

	service.checkOrigin(context.Background(), origin, checker)

	if replaceCallCount != 1 {
		t.Fatalf("ReplaceRecords was called %d times, expected 1", replaceCallCount)
	}
	if !sameStringSet(replaced, []string{"192.168.1.3"}) {
		t.Fatalf("expected fallback IPs, got %v", replaced)
	}
}

func TestServiceCheckOrigin_ReportsFailoverChecksAndHost(t *testing.T) {
	origin := config.OriginConfig{
		Name: "www", ZoneName: "default", RecordType: "A", ReturnToPriority: true,
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.0.2.1"}},
			{Priority: 50, IPs: []string{"192.0.2.2"}},
			{Priority: 0, IPs: []string{"192.0.2.3"}},
		},
	}
	service, dnsClientMock := createTestService(origin)
	dnsClientMock.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{Content: "192.0.2.1"}}, nil
	}
	dnsClientMock.ReplaceRecordsFunc = func(context.Context, string, string, []string) error {
		return nil
	}
	events := make(chan notifier.FailoverEvent, 1)
	service.notifiers = []notifier.Notifier{notifierFunc(func(_ context.Context, event notifier.FailoverEvent) error {
		events <- event
		return nil
	})}
	checker := hcmock.NewCheckerMock(func(ip string) error {
		switch ip {
		case "192.0.2.1":
			return fmt.Errorf("unexpected status code: 503 Service Unavailable")
		case "192.0.2.2":
			return fmt.Errorf("connection refused")
		default:
			return nil
		}
	})

	service.checkOrigin(context.Background(), origin, checker)
	select {
	case event := <-events:
		hostname, err := os.Hostname()
		if err != nil {
			t.Fatal(err)
		}
		if event.CheckerHostname != hostname {
			t.Errorf("checker hostname = %q, want %q", event.CheckerHostname, hostname)
		}
		want := []notifier.HealthCheckFailure{
			{Priority: 100, IP: "192.0.2.1", Reason: "unexpected status code: 503 Service Unavailable"},
			{Priority: 50, IP: "192.0.2.2", Reason: "connection refused"},
		}
		if len(event.HealthCheckFailures) != len(want) {
			t.Fatalf("failures = %v, want %v", event.HealthCheckFailures, want)
		}
		for i, got := range event.HealthCheckFailures {
			if got != want[i] {
				t.Errorf("failure %d = %+v, want %+v", i, got, want[i])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failover notification")
	}
}

func TestServiceCheckOrigin_ReturnToPriorityDisabled(t *testing.T) {
	origin := config.OriginConfig{
		Name:       "example.com",
		ZoneName:   "default",
		RecordType: "A",
		HealthCheck: config.HealthCheck{
			Type:     "http",
			Endpoint: "/health",
			Timeout:  5,
		},
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.168.1.1"}},
			{Priority: 50, IPs: []string{"192.168.1.2"}},
		},
		ReturnToPriority: false,
	}

	service, dnsClientMock := createTestService(origin)

	dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    dns.RecordResponseTypeA,
			Content: "192.168.1.2",
		}}, nil
	}

	replaceCallCount := 0
	dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType string, newContents []string) error {
		replaceCallCount++
		return nil
	}

	checker := hcmock.NewCheckerMock(func(ip string) error {
		return nil
	})

	service.checkOrigin(context.Background(), origin, checker)

	if replaceCallCount != 0 {
		t.Fatalf("ReplaceRecords was called %d times, expected 0", replaceCallCount)
	}
}

func TestServiceCheckOrigin_KeepsHealthyIPsAtCurrentPriority(t *testing.T) {
	origin := config.OriginConfig{
		Name:       "example.com",
		ZoneName:   "default",
		RecordType: "A",
		HealthCheck: config.HealthCheck{
			Type:     "http",
			Endpoint: "/health",
			Timeout:  5,
		},
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.168.1.1", "192.168.1.2"}},
			{Priority: 50, IPs: []string{"192.168.1.3"}},
		},
		ReturnToPriority: true,
	}

	service, dnsClientMock := createTestService(origin)

	dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{
			{ID: "record-1", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "192.168.1.1"},
			{ID: "record-2", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "192.168.1.2"},
		}, nil
	}

	replaceCallCount := 0
	var replaced []string
	dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType string, newContents []string) error {
		replaceCallCount++
		replaced = append([]string{}, newContents...)
		return nil
	}
	events := make(chan notifier.FailoverEvent, 1)
	service.notifiers = []notifier.Notifier{notifierFunc(func(_ context.Context, event notifier.FailoverEvent) error {
		events <- event
		return nil
	})}

	lowerPriorityChecks := 0
	checker := hcmock.NewCheckerMock(func(ip string) error {
		if ip == "192.168.1.1" {
			return fmt.Errorf("unhealthy")
		}
		if ip == "192.168.1.3" {
			lowerPriorityChecks++
		}
		return nil
	})

	service.checkOrigin(context.Background(), origin, checker)

	if replaceCallCount != 1 {
		t.Fatalf("ReplaceRecords was called %d times, expected 1", replaceCallCount)
	}
	if !sameStringSet(replaced, []string{"192.168.1.2"}) {
		t.Fatalf("expected healthy IP at priority 100, got %v", replaced)
	}
	if lowerPriorityChecks != 0 {
		t.Fatalf("checked lower priority %d times despite healthy IP at priority 100", lowerPriorityChecks)
	}
	select {
	case event := <-events:
		if !event.IsSamePriorityFailover || event.IsSamePriorityRecovery {
			t.Errorf("expected same-priority failover event, got %+v", event)
		}
		if len(event.HealthCheckFailures) != 1 || event.HealthCheckFailures[0].IP != "192.168.1.1" {
			t.Errorf("expected failed IP in notification, got %v", event.HealthCheckFailures)
		}
		if event.Reason != "Removing unhealthy IPs from priority level 100" {
			t.Errorf("unexpected notification reason: %q", event.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for same-priority notification")
	}
}

func TestServiceCheckOrigin_RestoresRecoveredIPAtSamePriority(t *testing.T) {
	origin := config.OriginConfig{
		Name: "example.com", ZoneName: "default", RecordType: "A",
		PriorityLevels: []config.PriorityLevel{
			{Priority: 100, IPs: []string{"192.168.1.1", "192.168.1.2"}},
			{Priority: 50, IPs: []string{"192.168.1.3"}},
		},
		ReturnToPriority: true,
	}
	service, dnsClientMock := createTestService(origin)
	dnsClientMock.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{Content: "192.168.1.2"}}, nil
	}
	var replaced []string
	dnsClientMock.ReplaceRecordsFunc = func(_ context.Context, _, _ string, ips []string) error {
		replaced = append([]string(nil), ips...)
		return nil
	}
	events := make(chan notifier.FailoverEvent, 1)
	service.notifiers = []notifier.Notifier{notifierFunc(func(_ context.Context, event notifier.FailoverEvent) error {
		events <- event
		return nil
	})}
	service.checkOrigin(context.Background(), origin, hcmock.NewCheckerMock(func(string) error { return nil }))
	if !sameStringSet(replaced, []string{"192.168.1.1", "192.168.1.2"}) {
		t.Fatalf("expected both recovered priority 100 IPs, got %v", replaced)
	}
	select {
	case event := <-events:
		if !event.IsSamePriorityRecovery || event.IsSamePriorityFailover {
			t.Errorf("expected same-priority recovery event, got %+v", event)
		}
		if len(event.HealthCheckFailures) != 0 {
			t.Errorf("unexpected health check failures: %v", event.HealthCheckFailures)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovery notification")
	}
}

func sameStringSet(a, b []string) bool {
	setA := make(map[string]struct{}, len(a))
	for _, v := range a {
		setA[v] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, v := range b {
		setB[v] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for v := range setA {
		if _, ok := setB[v]; !ok {
			return false
		}
	}
	return true
}
