package gslb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	"github.com/bootjp/cloudflare-gslb/pkg/notifier"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

func TestCNAMENotificationUsesCandidateNames(t *testing.T) {
	origin := config.OriginConfig{Name: "app.example.com", ZoneName: "default", RecordType: "CNAME", Proxied: true, ReturnToPriority: true, CNAMETargets: []config.CNAMETarget{{Name: "primary", Priority: 100, Target: "primary.example.net", ProbeURL: "https://primary.example.net/healthz"}, {Name: "backup", Priority: 0, Target: "backup.example.org", ProbeURL: "https://backup.example.org/healthz"}}}
	s, m := createTestService(origin)
	current := "PRIMARY.EXAMPLE.NET."
	m.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{ID: "managed", Type: dns.RecordResponseTypeCNAME, Content: current, Proxied: true}}, nil
	}
	m.UpdateDNSRecordFunc = func(_ context.Context, _ string, _ string, _ string, target string) (dns.RecordResponse, error) {
		current = target
		return dns.RecordResponse{}, nil
	}
	events := make(chan notifier.FailoverEvent, 1)
	s.notifiers = []notifier.Notifier{notifierFunc(func(_ context.Context, event notifier.FailoverEvent) error { events <- event; return nil })}
	checker := hcmock.NewCheckerMock(func(probe string) error {
		if probe == origin.CNAMETargets[0].ProbeURL {
			return fmt.Errorf("connection refused")
		}
		return nil
	})
	if err := s.checkCNAMEOrigin(context.Background(), origin, checker); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.OldTargetName != "primary" || event.NewTargetName != "backup" {
			t.Fatalf("names missing: %+v", event)
		}
		if event.OldIP != "primary.example.net" || event.NewIP != "backup.example.org" {
			t.Fatalf("raw targets changed: %+v", event)
		}
		if len(event.HealthCheckFailures) != 1 || event.HealthCheckFailures[0].TargetName != "primary" {
			t.Fatalf("failed target name missing: %+v", event.HealthCheckFailures)
		}
	case <-time.After(time.Second):
		t.Fatal("notification missing")
	}
}
