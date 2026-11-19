package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func cnameOrigin() OriginConfig {
	return OriginConfig{Name: "app.example.com", ZoneName: "example.com", RecordType: "CNAME", Proxied: true, ReturnToPriority: true,
		HealthCheck:  HealthCheck{Type: "url", Timeout: 5},
		CNAMETargets: []CNAMETarget{{Name: "primary", Priority: 100, Target: "primary.example.net", ProbeURL: "https://probe-primary.example.com/healthz"}, {Name: "backup", Priority: 0, Target: "backup.example.net", ProbeURL: "https://probe-backup.example.com/healthz"}}}
}

func TestCNAMEConfigRoundTrip(t *testing.T) {
	cfg := rawConfig{DryRun: true, CheckInterval: 30, CloudflareZoneIDs: []ZoneConfig{{Name: "example.com", ZoneID: "zone"}}, Origins: []OriginConfig{cnameOrigin()}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.DryRun || len(loaded.Origins[0].CNAMETargets) != 2 {
		t.Fatal("lost CNAME targets or dry-run setting")
	}
}

func TestRejectUnsafeCNAMEConfigs(t *testing.T) {
	cases := map[string]func(*OriginConfig){
		"mixed IPs":              func(o *OriginConfig) { o.PriorityLevels = []PriorityLevel{{Priority: 100, IPs: []string{"127.0.0.1"}}} },
		"duplicate priority":     func(o *OriginConfig) { o.CNAMETargets[1].Priority = 100 },
		"duplicate target":       func(o *OriginConfig) { o.CNAMETargets[1].Target = o.CNAMETargets[0].Target },
		"TLS bypass":             func(o *OriginConfig) { o.HealthCheck.InsecureSkipVerify = true },
		"wrong Host":             func(o *OriginConfig) { o.HealthCheck.Host = o.Name },
		"zero timeout":           func(o *OriginConfig) { o.HealthCheck.Timeout = 0 },
		"public selection probe": func(o *OriginConfig) { o.CNAMETargets[0].ProbeURL = "https://" + o.Name + "/healthz" },
		"credentials":            func(o *OriginConfig) { o.CNAMETargets[0].ProbeURL = "https://user:secret@probe.example.com/healthz" },
		"insecure probe":         func(o *OriginConfig) { o.CNAMETargets[0].ProbeURL = "http://probe.example.com/healthz" },
		"IP target":              func(o *OriginConfig) { o.CNAMETargets[0].Target = "192.0.2.1" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := cnameOrigin()
			mutate(&o)
			if err := normalizeOriginMode(&o); err == nil {
				t.Fatal("accepted unsafe configuration")
			}
		})
	}
}

func TestCNAMEProbeIDNAIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, origin, probe string
		wantErr             bool
	}{
		{"Unicode probe aliases ASCII origin", "xn--bcher-kva.example.com", "https://bücher.example.com/healthz", true},
		{"ASCII probe aliases Unicode origin", "bücher.example.com", "https://xn--bcher-kva.example.com/healthz", true},
		{"case and trailing dot", "XN--BCHER-KVA.EXAMPLE.COM.", "https://BÜCHER.example.com/healthz", true},
		{"mapped DNS separator", "app.example.com", "https://app。example.com/healthz", true},
		{"invalid probe IDNA", "app.example.com", "https://foo\u200d.example.com/healthz", true},
		{"invalid origin IDNA", "foo\u200d.example.com", "https://probe.example.com/healthz", true},
		{"different Unicode probe", "xn--bcher-kva.example.com", "https://probe.bücher.example.com/healthz", false},
		{"different ASCII probe", "bücher.example.com", "https://probe.example.com/healthz", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCNAMEProbe(tc.origin, CNAMETarget{Name: "primary", ProbeURL: tc.probe})
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateCNAMEProbe() error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func loadCNAMEFixture(t *testing.T, origin OriginConfig, extension string) (*Config, error) {
	t.Helper()
	cfg := rawConfig{DryRun: true, CheckInterval: 30, CloudflareZoneIDs: []ZoneConfig{{Name: "example.com", ZoneID: "zone"}}, Origins: []OriginConfig{origin}}
	marshal := json.Marshal
	if extension == ".yaml" {
		marshal = yaml.Marshal
	}
	data, err := marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config"+extension)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(path)
}

func TestCNAMEArbitraryDestinations(t *testing.T) {
	for _, extension := range []string{".json", ".yaml"} {
		for _, proxied := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/proxied=%t", extension, proxied), func(t *testing.T) {
				origin := cnameOrigin()
				origin.Name = "APP.Example.com."
				origin.Proxied = proxied
				origin.CNAMETargets[0].Target = "BÜCHER.example.net."
				origin.CNAMETargets[0].ProbeURL = "https://bücher.example.net/healthz"
				origin.CNAMETargets[1].Target = "provider.example.org"
				cfg, err := loadCNAMEFixture(t, origin, extension)
				if err != nil {
					t.Fatal(err)
				}
				got := cfg.Origins[0]
				if got.Name != "app.example.com" || got.Proxied != proxied || got.CNAMETargets[0].Target != "xn--bcher-kva.example.net" || got.CNAMETargets[1].Target != "provider.example.org" {
					t.Fatalf("unexpected normalized origin: %+v", got)
				}
			})
		}
	}
}

func TestCNAMERejectInvalidDestinations(t *testing.T) {
	for _, target := range []string{"", "192.0.2.1", "2001:db8::1", "https://origin.example.net", "origin.example.net:443", "origin..example.net", "-bad.example.net", "foo\u200d.example.net", strings.Repeat("a", 64) + ".example.net", "APP.EXAMPLE.COM."} {
		t.Run(target, func(t *testing.T) {
			origin := cnameOrigin()
			origin.CNAMETargets[0].Target = target
			if err := normalizeOriginMode(&origin); err == nil {
				t.Fatal("accepted invalid or self-referential CNAME target")
			}
		})
	}
	origin := cnameOrigin()
	origin.CNAMETargets[0].Target = "bücher.example.net"
	origin.CNAMETargets[1].Target = "XN--BCHER-KVA.example.net."
	if err := normalizeOriginMode(&origin); err == nil {
		t.Fatal("accepted duplicate IDNA target")
	}
}
