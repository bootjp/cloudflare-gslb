package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tunnelOrigin() OriginConfig {
	return OriginConfig{Name: "app.example.com", ZoneName: "example.com", RecordType: "CNAME", Proxied: true, ReturnToPriority: true,
		HealthCheck:  HealthCheck{Type: "url", Timeout: 5},
		TunnelGroups: []TunnelGroup{{Name: "primary", Priority: 100, Target: "11111111-1111-1111-1111-111111111111.cfargotunnel.com", ProbeURL: "https://probe-primary.example.com/healthz"}, {Name: "backup", Priority: 0, Target: "22222222-2222-2222-2222-222222222222.cfargotunnel.com", ProbeURL: "https://probe-backup.example.com/healthz"}}}
}

func TestTunnelConfigRoundTrip(t *testing.T) {
	cfg := rawConfig{DryRun: true, CheckInterval: 30, CloudflareZoneIDs: []ZoneConfig{{Name: "example.com", ZoneID: "zone"}}, Origins: []OriginConfig{tunnelOrigin()}}
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
	if !loaded.DryRun || len(loaded.Origins[0].TunnelGroups) != 2 {
		t.Fatal("lost Tunnel groups or dry-run setting")
	}
}

func TestRejectUnsafeTunnelConfigs(t *testing.T) {
	cases := map[string]func(*OriginConfig){
		"mixed IPs":              func(o *OriginConfig) { o.PriorityLevels = []PriorityLevel{{Priority: 100, IPs: []string{"127.0.0.1"}}} },
		"duplicate priority":     func(o *OriginConfig) { o.TunnelGroups[1].Priority = 100 },
		"duplicate UUID":         func(o *OriginConfig) { o.TunnelGroups[1].Target = o.TunnelGroups[0].Target },
		"not proxied":            func(o *OriginConfig) { o.Proxied = false },
		"TLS bypass":             func(o *OriginConfig) { o.HealthCheck.InsecureSkipVerify = true },
		"wrong Host":             func(o *OriginConfig) { o.HealthCheck.Host = o.Name },
		"zero timeout":           func(o *OriginConfig) { o.HealthCheck.Timeout = 0 },
		"public selection probe": func(o *OriginConfig) { o.TunnelGroups[0].ProbeURL = "https://" + o.Name + "/healthz" },
		"credentials":            func(o *OriginConfig) { o.TunnelGroups[0].ProbeURL = "https://user:secret@probe.example.com/healthz" },
		"insecure probe":         func(o *OriginConfig) { o.TunnelGroups[0].ProbeURL = "http://probe.example.com/healthz" },
		"UUID probe":             func(o *OriginConfig) { o.TunnelGroups[0].ProbeURL = "https://" + o.TunnelGroups[0].Target + "/healthz" },
		"invalid UUID":           func(o *OriginConfig) { o.TunnelGroups[0].Target = "not-a-tunnel.example.com" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := tunnelOrigin()
			mutate(&o)
			if err := validateOriginMode(o); err == nil {
				t.Fatal("accepted unsafe configuration")
			}
		})
	}
}

func TestTunnelProbeIDNAIdentity(t *testing.T) {
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
			err := validateTunnelProbe(tc.origin, TunnelGroup{Name: "primary", ProbeURL: tc.probe})
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateTunnelProbe() error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
