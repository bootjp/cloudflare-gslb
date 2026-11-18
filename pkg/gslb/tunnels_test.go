package gslb

import (
	"context"
	"fmt"
	"testing"

	"github.com/bootjp/cloudflare-gslb/config"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

func TestTunnelTierFailoverAndReadBack(t *testing.T) {
	for _, tc := range []struct {
		name             string
		current          int
		failed           map[string]bool
		returnToPriority bool
		dryRun           bool
		drift            bool
		missing          bool
		mismatch         bool
		want             int
		wantErr          bool
	}{
		{name: "primary stays selected", current: 0, returnToPriority: true, want: -1},
		{name: "primary fails", current: 0, failed: map[string]bool{"primary": true}, returnToPriority: true, want: 1},
		{name: "primary recovers", current: 1, returnToPriority: true, want: 0},
		{name: "failback disabled", current: 1, want: -1},
		{name: "all fail", current: 0, failed: map[string]bool{"primary": true, "backup": true}, want: -1, wantErr: true},
		{name: "dry-run", current: 0, failed: map[string]bool{"primary": true}, returnToPriority: true, dryRun: true, want: -1},
		{name: "shadow before migration", missing: true, dryRun: true, returnToPriority: true, want: -1},
		{name: "missing CNAME", missing: true, want: -1, wantErr: true},
		{name: "manual drift", drift: true, want: -1, wantErr: true},
		{name: "readback mismatch", current: 0, failed: map[string]bool{"primary": true}, returnToPriority: true, mismatch: true, want: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := []config.TunnelGroup{{Name: "primary", Priority: 100, Target: "11111111-1111-1111-1111-111111111111.cfargotunnel.com", ProbeURL: "https://primary-probe.example.com/healthz"}, {Name: "backup", Priority: 0, Target: "22222222-2222-2222-2222-222222222222.cfargotunnel.com", ProbeURL: "https://backup-probe.example.com/healthz"}}
			o := config.OriginConfig{Name: "app.example.com", ZoneName: "default", RecordType: "CNAME", Proxied: true, ReturnToPriority: tc.returnToPriority, TunnelGroups: groups}
			s, m := createTestService(o)
			s.config.DryRun = tc.dryRun
			current := groups[tc.current].Target
			if tc.drift {
				current = "unknown.example.com"
			}
			m.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
				if tc.missing {
					return nil, nil
				}
				return []dns.RecordResponse{{ID: "managed", Content: current, Type: dns.RecordResponseTypeCNAME, Proxied: true}}, nil
			}
			calls := 0
			m.ReplaceRecordsFunc = func(_ context.Context, _ string, typ string, values []string) error {
				calls++
				if typ != "CNAME" || len(values) != 1 {
					t.Fatalf("invalid update %s %v", typ, values)
				}
				if tc.want < 0 || values[0] != groups[tc.want].Target {
					t.Fatalf("unexpected selection %v", values)
				}
				if !tc.mismatch {
					current = values[0]
				}
				return nil
			}
			checker := hcmock.NewCheckerMock(func(probe string) error {
				for _, g := range groups {
					if probe == g.ProbeURL {
						if tc.failed[g.Name] {
							return fmt.Errorf("not ready")
						}
						return nil
					}
				}
				return fmt.Errorf("not a fixed group probe")
			})
			err := s.checkTunnelOrigin(context.Background(), o, checker)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
			wantCalls := 0
			if tc.want >= 0 {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("writes=%d want %d", calls, wantCalls)
			}
		})
	}
}

func TestDryRunAlsoProtectsExistingIPOrigins(t *testing.T) {
	origin := config.OriginConfig{Name: "app.example.com", ZoneName: "default", RecordType: "A", PriorityLevels: []config.PriorityLevel{{Priority: 100, IPs: []string{"192.0.2.1"}}}}
	s, m := createTestService(origin)
	s.config.DryRun = true
	m.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
		return []dns.RecordResponse{{ID: "old", Type: dns.RecordResponseTypeA, Content: "192.0.2.2"}}, nil
	}
	m.ReplaceRecordsFunc = func(context.Context, string, string, []string) error {
		t.Fatal("dry-run changed an existing IP origin")
		return nil
	}
	s.checkOrigin(context.Background(), origin, hcmock.NewCheckerMock(func(string) error { return nil }))
}
