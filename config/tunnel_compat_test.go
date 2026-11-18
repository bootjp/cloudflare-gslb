package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func legacyTunnelOrigin() OriginConfig {
	origin := cnameOrigin()
	origin.TunnelGroups = origin.CNAMETargets
	origin.CNAMETargets = nil
	origin.TunnelGroups[0].Target = "11111111-1111-1111-1111-111111111111.cfargotunnel.com"
	origin.TunnelGroups[1].Target = "22222222-2222-2222-2222-222222222222.cfargotunnel.com"
	return origin
}

func TestLegacyTunnelConfigCompatibility(t *testing.T) {
	for _, extension := range []string{".json", ".yaml"} {
		t.Run(extension, func(t *testing.T) {
			origin := legacyTunnelOrigin()
			cfg, err := loadCNAMEFixture(t, origin, extension)
			if err != nil {
				t.Fatal(err)
			}
			got := cfg.Origins[0]
			if len(got.TunnelGroups) != 0 || len(got.CNAMETargets) != 2 || got.CNAMETargets[0] != origin.TunnelGroups[0] {
				t.Fatalf("legacy configuration was not normalized: %+v", got)
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "tunnel_groups") {
				t.Fatal("serialized config retained legacy fields")
			}
		})
	}
}

func TestLegacyTunnelConfigRejectsAmbiguousModes(t *testing.T) {
	for name, mutate := range map[string]func(*OriginConfig){
		"both fields":              func(o *OriginConfig) { o.CNAMETargets = []CNAMETarget{{Name: "other"}} },
		"IP mode":                  func(o *OriginConfig) { o.RecordType = "A" },
		"DNS-only legacy tunnel":   func(o *OriginConfig) { o.Proxied = false },
		"non-Tunnel legacy target": func(o *OriginConfig) { o.TunnelGroups[0].Target = "origin.example.net" },
		"UUID probe":               func(o *OriginConfig) { o.TunnelGroups[0].ProbeURL = "https://" + o.TunnelGroups[0].Target + "/healthz" },
	} {
		t.Run(name, func(t *testing.T) {
			origin := legacyTunnelOrigin()
			mutate(&origin)
			if _, err := loadCNAMEFixture(t, origin, ".json"); err == nil {
				t.Fatal("accepted ambiguous or invalid legacy config")
			}
		})
	}
}
