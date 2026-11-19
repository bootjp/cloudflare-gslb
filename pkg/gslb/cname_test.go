package gslb

import (
	"context"
	"fmt"
	"testing"

	"github.com/bootjp/cloudflare-gslb/config"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

func TestCNAMEPriorityFailoverAndReadBack(t *testing.T) {
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
		for _, proxied := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/proxied=%t", tc.name, proxied), func(t *testing.T) {
				candidates := []config.CNAMETarget{{Name: "primary", Priority: 100, Target: "primary.example.net", ProbeURL: "https://primary-probe.example.com/healthz"}, {Name: "backup", Priority: 0, Target: "backup.example.net", ProbeURL: "https://backup-probe.example.com/healthz"}}
				o := config.OriginConfig{Name: "app.example.com", ZoneName: "default", RecordType: "CNAME", Proxied: proxied, ReturnToPriority: tc.returnToPriority, CNAMETargets: candidates}
				s, m := createTestService(o)
				s.config.DryRun = tc.dryRun
				current := candidates[tc.current].Target
				if tc.drift {
					current = "unknown.example.com"
				}
				m.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
					if tc.missing {
						return nil, nil
					}
					return []dns.RecordResponse{{ID: "managed", Content: current, Type: dns.RecordResponseTypeCNAME, Proxied: proxied}}, nil
				}
				calls := 0
				m.ReplaceRecordsFunc = func(context.Context, string, string, []string) error {
					t.Fatal("CNAME update must use the verified record ID")
					return nil
				}
				m.UpdateDNSRecordFunc = func(_ context.Context, id, name, typ, value string) (dns.RecordResponse, error) {
					calls++
					if id != "managed" || name != o.Name || typ != "CNAME" {
						t.Fatalf("invalid update id=%s name=%s type=%s", id, name, typ)
					}
					if tc.want < 0 || value != candidates[tc.want].Target {
						t.Fatalf("unexpected selection %s", value)
					}
					if !tc.mismatch {
						current = value
					}
					return dns.RecordResponse{ID: id, Content: value, Type: dns.RecordResponseTypeCNAME, Proxied: proxied}, nil
				}
				checker := hcmock.NewCheckerMock(func(probe string) error {
					for _, g := range candidates {
						if probe == g.ProbeURL {
							if tc.failed[g.Name] {
								return fmt.Errorf("not ready")
							}
							return nil
						}
					}
					return fmt.Errorf("not a fixed group probe")
				})
				err := s.checkCNAMEOrigin(context.Background(), o, checker)
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

func TestCNAMERefusesDNSChangesDuringProbe(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*dns.RecordResponse)
		missing bool
		readErr bool
	}{
		{name: "target changed", mutate: func(r *dns.RecordResponse) { r.Content = "manual.example.com" }},
		{name: "target changed to another managed tier", mutate: func(r *dns.RecordResponse) { r.Content = "primary.example.net" }},
		{name: "record recreated", mutate: func(r *dns.RecordResponse) { r.ID = "replacement" }},
		{name: "proxy changed", mutate: func(r *dns.RecordResponse) { r.Proxied = false }},
		{name: "record removed", missing: true},
		{name: "read failed", readErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidates := []config.CNAMETarget{
				{Name: "primary", Priority: 100, Target: "primary.example.net", ProbeURL: "https://primary-probe.example.com/healthz"},
				{Name: "backup", Priority: 0, Target: "backup.example.net", ProbeURL: "https://backup-probe.example.com/healthz"},
			}
			origin := config.OriginConfig{Name: "app.example.com", ZoneName: "default", RecordType: "CNAME", Proxied: true, ReturnToPriority: true, CNAMETargets: candidates}
			s, m := createTestService(origin)
			current := dns.RecordResponse{ID: "managed", Type: dns.RecordResponseTypeCNAME, Content: candidates[1].Target, Proxied: true}
			probed := false
			m.GetDNSRecordsFunc = func(context.Context, string, string) ([]dns.RecordResponse, error) {
				if probed && tc.readErr {
					return nil, fmt.Errorf("read unavailable")
				}
				if probed && tc.missing {
					return nil, nil
				}
				return []dns.RecordResponse{current}, nil
			}
			writes := 0
			m.ReplaceRecordsFunc = func(context.Context, string, string, []string) error { writes++; return nil }
			m.UpdateDNSRecordFunc = func(context.Context, string, string, string, string) (dns.RecordResponse, error) {
				writes++
				return current, nil
			}
			checker := hcmock.NewCheckerMock(func(string) error {
				probed = true
				if tc.mutate != nil {
					tc.mutate(&current)
				}
				return nil
			})
			if err := s.checkCNAMEOrigin(context.Background(), origin, checker); err == nil {
				t.Error("accepted DNS changed during probe")
			}
			if writes != 0 {
				t.Fatalf("overwrote concurrent DNS edit: writes=%d", writes)
			}
		})
	}
}
