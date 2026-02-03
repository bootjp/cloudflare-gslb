package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
)

type migrateOrigin struct {
	Name             string                 `json:"name"`
	ZoneName         string                 `json:"zone_name,omitempty"`
	RecordType       string                 `json:"record_type"`
	HealthCheck      config.HealthCheck     `json:"health_check"`
	PriorityLevels   []config.PriorityLevel `json:"priority_levels,omitempty"`
	Proxied          bool                   `json:"proxied"`
	ReturnToPriority bool                   `json:"return_to_priority"`
}

type migrateConfig struct {
	CloudflareAPIToken string                      `json:"cloudflare_api_token"`
	CloudflareZoneIDs  []config.ZoneConfig         `json:"cloudflare_zones"`
	CheckInterval      int                         `json:"check_interval_seconds"`
	Origins            []migrateOrigin             `json:"origins"`
	Notifications      []config.NotificationConfig `json:"notifications,omitempty"`
}

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	outPath := flag.String("out", "config.migrated.json", "Path to write migrated config")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	origins := make([]migrateOrigin, 0, len(cfg.Origins))
	for _, origin := range cfg.Origins {
		origins = append(origins, migrateOrigin{
			Name:             origin.Name,
			ZoneName:         origin.ZoneName,
			RecordType:       origin.RecordType,
			HealthCheck:      origin.HealthCheck,
			PriorityLevels:   origin.EffectivePriorityLevels(),
			Proxied:          origin.Proxied,
			ReturnToPriority: origin.ReturnToPriority,
		})
	}

	out := migrateConfig{
		CloudflareAPIToken: cfg.CloudflareAPIToken,
		CloudflareZoneIDs:  cfg.CloudflareZoneIDs,
		CheckInterval:      int(cfg.CheckInterval / time.Second),
		Origins:            origins,
		Notifications:      cfg.Notifications,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal migrated config: %v", err)
	}

	if err := os.WriteFile(*outPath, data, 0o600); err != nil {
		log.Fatalf("Failed to write migrated config: %v", err)
	}

	log.Printf("Migrated config written to %s", *outPath)
}
