package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/bootjp/cloudflare-gslb/config"
)

func main() {
	inputPath := flag.String("input", "", "Path to input configuration file (required)")
	outputPath := flag.String("output", "", "Path to output configuration file (if not specified, output to stdout)")
	flag.Parse()

	if *inputPath == "" {
		flag.Usage()
		log.Fatal("Error: -input flag is required")
	}

	migratedConfig, err := migrateConfig(*inputPath)
	if err != nil {
		log.Fatalf("Failed to migrate config: %v", err)
	}

	// JSONとして出力（インデント付き）
	output, err := json.MarshalIndent(migratedConfig, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal migrated config: %v", err)
	}

	if *outputPath == "" {
		// 標準出力に出力
		fmt.Println(string(output))
	} else {
		// ファイルに出力
		if err := os.WriteFile(*outputPath, output, 0644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}
		log.Printf("Successfully migrated config to %s", *outputPath)
	}
}

// migratedConfig は出力用の設定構造体
type migratedConfig struct {
	CloudflareAPIToken string                    `json:"cloudflare_api_token"`
	CloudflareZoneID   string                    `json:"cloudflare_zone_id,omitempty"`
	CloudflareZoneIDs  []config.ZoneConfig       `json:"cloudflare_zones,omitempty"`
	CheckInterval      int                       `json:"check_interval_seconds"`
	Origins            []migratedOriginConfig    `json:"origins"`
	Notifications      []config.NotificationConfig `json:"notifications,omitempty"`
}

// migratedOriginConfig は出力用のオリジン設定構造体
type migratedOriginConfig struct {
	Name                string              `json:"name"`
	ZoneName            string              `json:"zone_name,omitempty"`
	RecordType          string              `json:"record_type"`
	HealthCheck         config.HealthCheck  `json:"health_check"`
	PriorityFailoverIPs []config.PriorityIP `json:"priority_failover_ips,omitempty"`
	FailoverIPs         []string            `json:"failover_ips,omitempty"`
	Proxied             bool                `json:"proxied"`
	ReturnToPriority    bool                `json:"return_to_priority"`
}

// rawConfig は古い形式の設定を読み込むための構造体
type rawConfig struct {
	CloudflareAPIToken string                    `json:"cloudflare_api_token"`
	CloudflareZoneID   string                    `json:"cloudflare_zone_id"`
	CloudflareZoneIDs  []config.ZoneConfig       `json:"cloudflare_zones"`
	CheckInterval      int                       `json:"check_interval_seconds"`
	Origins            []rawOriginConfig         `json:"origins"`
	Notifications      []config.NotificationConfig `json:"notifications"`
}

// rawOriginConfig は古い形式のオリジン設定を読み込むための構造体
type rawOriginConfig struct {
	Name                string             `json:"name"`
	ZoneName            string             `json:"zone_name"`
	RecordType          string             `json:"record_type"`
	HealthCheck         config.HealthCheck `json:"health_check"`
	PriorityFailoverIPs json.RawMessage    `json:"priority_failover_ips"`
	FailoverIPs         []string           `json:"failover_ips"`
	Proxied             bool               `json:"proxied"`
	ReturnToPriority    bool               `json:"return_to_priority"`
}

func migrateConfig(inputPath string) (*migratedConfig, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

	var raw rawConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode input config: %w", err)
	}

	migrated := &migratedConfig{
		CloudflareAPIToken: raw.CloudflareAPIToken,
		CloudflareZoneID:   raw.CloudflareZoneID,
		CloudflareZoneIDs:  raw.CloudflareZoneIDs,
		CheckInterval:      raw.CheckInterval,
		Origins:            make([]migratedOriginConfig, len(raw.Origins)),
		Notifications:      raw.Notifications,
	}

	for i, rawOrigin := range raw.Origins {
		priorityIPs, needsMigration, err := migratePriorityIPs(rawOrigin.PriorityFailoverIPs)
		if err != nil {
			return nil, fmt.Errorf("origin %s: failed to migrate priority IPs: %w", rawOrigin.Name, err)
		}

		if needsMigration {
			log.Printf("Origin '%s': Migrated %d priority IPs to new format", rawOrigin.Name, len(priorityIPs))
		}

		migrated.Origins[i] = migratedOriginConfig{
			Name:                rawOrigin.Name,
			ZoneName:            rawOrigin.ZoneName,
			RecordType:          rawOrigin.RecordType,
			HealthCheck:         rawOrigin.HealthCheck,
			PriorityFailoverIPs: priorityIPs,
			FailoverIPs:         rawOrigin.FailoverIPs,
			Proxied:             rawOrigin.Proxied,
			ReturnToPriority:    rawOrigin.ReturnToPriority,
		}
	}

	return migrated, nil
}

// migratePriorityIPs は priority_failover_ips を新しい形式にマイグレーションする
// 戻り値: (マイグレーション後のIP、マイグレーションが必要だったかどうか、エラー)
func migratePriorityIPs(raw json.RawMessage) ([]config.PriorityIP, bool, error) {
	if raw == nil || len(raw) == 0 {
		return nil, false, nil
	}

	// まず新しい形式（PriorityIP配列）でパースを試みる
	var priorityIPs []config.PriorityIP
	if err := json.Unmarshal(raw, &priorityIPs); err == nil {
		// PriorityIP形式でパースできた場合、すべての要素のIPが空でないかチェック
		isNewFormat := len(priorityIPs) > 0
		for _, p := range priorityIPs {
			if p.IP == "" {
				isNewFormat = false
				break
			}
		}
		if isNewFormat {
			// すでに新しい形式なのでマイグレーション不要
			return priorityIPs, false, nil
		}
	}

	// 古い形式（文字列配列）でパースを試みる
	var ips []string
	if err := json.Unmarshal(raw, &ips); err != nil {
		return nil, false, fmt.Errorf("failed to parse priority_failover_ips: %w", err)
	}

	// 文字列配列をPriorityIP配列に変換
	// 新しい優先度の順序（大きいほど優先）に従って、最初のIPが最も高い優先度を持つ
	priorityIPs = make([]config.PriorityIP, len(ips))
	for i, ip := range ips {
		// 最初のIPが最も高い優先度（最大値）を持つ
		priorityIPs[i] = config.PriorityIP{
			IP:       ip,
			Priority: len(ips) - 1 - i,
		}
	}

	return priorityIPs, true, nil
}
