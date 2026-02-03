package gslb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	cfmock "github.com/bootjp/cloudflare-gslb/pkg/cloudflare/mock"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

type MockDNSClient struct {
	*cfmock.DNSClientMock
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

func TestServiceCheckOrigin_FallbackWhenPriorityLevelPartiallyUnhealthy(t *testing.T) {
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

	checker := hcmock.NewCheckerMock(func(ip string) error {
		if ip == "192.168.1.1" {
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
