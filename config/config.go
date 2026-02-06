package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config はアプリケーションの設定を表す構造体
type Config struct {
	CloudflareAPIToken string               `json:"cloudflare_api_token" yaml:"cloudflare_api_token"`
	CloudflareZoneIDs  []ZoneConfig         `json:"cloudflare_zones" yaml:"cloudflare_zones"`
	CheckInterval      time.Duration        `json:"check_interval_seconds" yaml:"check_interval_seconds"`
	Origins            []OriginConfig       `json:"origins" yaml:"origins"`
	Notifications      []NotificationConfig `json:"notifications" yaml:"notifications"` // 通知設定
}

// ZoneConfig はCloudflareゾーンの設定を表す構造体
type ZoneConfig struct {
	ZoneID string `json:"zone_id" yaml:"zone_id"`
	Name   string `json:"name" yaml:"name"`
}

// OriginConfig はオリジンサーバーの設定を表す構造体
type OriginConfig struct {
	Name                string          `json:"name" yaml:"name"`
	ZoneName            string          `json:"zone_name" yaml:"zone_name"`     // 対象のゾーン名
	RecordType          string          `json:"record_type" yaml:"record_type"` // "A" または "AAAA"
	HealthCheck         HealthCheck     `json:"health_check" yaml:"health_check"`
	PriorityLevels      []PriorityLevel `json:"priority_levels,omitempty" yaml:"priority_levels,omitempty"`             // 優先度付きIPグループ（高い値ほど優先）
	PriorityFailoverIPs []string        `json:"priority_failover_ips,omitempty" yaml:"priority_failover_ips,omitempty"` // 互換用: 優先的に使用するフェイルオーバー用のIPアドレスリスト
	FailoverIPs         []string        `json:"failover_ips,omitempty" yaml:"failover_ips,omitempty"`                   // 互換用: フェイルオーバー用のIPアドレスリスト
	Proxied             bool            `json:"proxied" yaml:"proxied"`                                                 // Cloudflareのプロキシを有効にするかどうか
	ReturnToPriority    bool            `json:"return_to_priority" yaml:"return_to_priority"`                           // 正常に戻ったときに優先IPに戻すかどうか
}

// PriorityLevel は優先度付きIPグループを表す構造体
type PriorityLevel struct {
	Priority int      `json:"priority" yaml:"priority"`
	IPs      []string `json:"ips" yaml:"ips"`
}

const (
	// LegacyPriorityHigh は旧形式のpriority_failover_ipsに割り当てる優先度
	LegacyPriorityHigh = 100
	// LegacyPriorityLow は旧形式のfailover_ipsに割り当てる優先度
	LegacyPriorityLow = 0
)

// EffectivePriorityLevels は新旧両形式を統合した優先度付きIPグループを返す
func (o OriginConfig) EffectivePriorityLevels() []PriorityLevel {
	levels := NormalizePriorityLevels(o.PriorityLevels)
	if len(levels) > 0 {
		return levels
	}
	return NormalizePriorityLevels(legacyPriorityLevels(o.PriorityFailoverIPs, o.FailoverIPs))
}

func legacyPriorityLevels(priorityIPs, failoverIPs []string) []PriorityLevel {
	levels := make([]PriorityLevel, 0, 2)
	if len(priorityIPs) > 0 {
		levels = append(levels, PriorityLevel{
			Priority: LegacyPriorityHigh,
			IPs:      priorityIPs,
		})
	}
	if len(failoverIPs) > 0 {
		levels = append(levels, PriorityLevel{
			Priority: LegacyPriorityLow,
			IPs:      failoverIPs,
		})
	}
	return levels
}

// NormalizePriorityLevels は同一優先度のIPを統合し、重複を除去する
func NormalizePriorityLevels(levels []PriorityLevel) []PriorityLevel {
	if len(levels) == 0 {
		return nil
	}

	merged := make(map[int][]string)
	order := make([]int, 0, len(levels))

	for _, level := range levels {
		if len(level.IPs) == 0 {
			continue
		}
		if _, exists := merged[level.Priority]; !exists {
			order = append(order, level.Priority)
		}
		merged[level.Priority] = append(merged[level.Priority], level.IPs...)
	}

	normalized := make([]PriorityLevel, 0, len(order))
	for _, priority := range order {
		ips := uniqueStrings(merged[priority])
		if len(ips) == 0 {
			continue
		}
		normalized = append(normalized, PriorityLevel{
			Priority: priority,
			IPs:      ips,
		})
	}

	return normalized
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// HealthCheck はヘルスチェックの設定を表す構造体
type HealthCheck struct {
	Type               string            `json:"type" yaml:"type"`                                 // "http", "https", "icmp"
	Endpoint           string            `json:"endpoint" yaml:"endpoint"`                         // HTTPSの場合のパス
	Host               string            `json:"host" yaml:"host"`                                 // HTTPSの場合のホスト名
	Timeout            int               `json:"timeout" yaml:"timeout"`                           // タイムアウト（秒）
	InsecureSkipVerify bool              `json:"insecure_skip_verify" yaml:"insecure_skip_verify"` // HTTPSの場合に証明書検証をスキップするかどうか
	Headers            map[string]string `json:"headers" yaml:"headers"`                           // ヘルスチェックリクエストに追加するHTTPヘッダ
}

// NotificationConfig は通知設定を表す構造体
type NotificationConfig struct {
	Type       string `json:"type" yaml:"type"`               // "slack" または "discord"
	WebhookURL string `json:"webhook_url" yaml:"webhook_url"` // WebhookのURL
}

// LoadConfig は設定ファイルを読み込む関数
func LoadConfig(path string) (*Config, error) {
	// Check if path is a directory
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	tmpConfig, err := decodeConfig(path, data)
	if err != nil {
		return nil, err
	}

	config := buildConfig(tmpConfig)
	applyLegacyZoneConfig(config, tmpConfig)
	if err := normalizeOrigins(config); err != nil {
		return nil, err
	}

	return config, nil
}

type rawConfig struct {
	CloudflareAPIToken string               `json:"cloudflare_api_token" yaml:"cloudflare_api_token"`
	CloudflareZoneID   string               `json:"cloudflare_zone_id" yaml:"cloudflare_zone_id"`
	CloudflareZoneIDs  []ZoneConfig         `json:"cloudflare_zones" yaml:"cloudflare_zones"`
	CheckInterval      int                  `json:"check_interval_seconds" yaml:"check_interval_seconds"`
	Origins            []OriginConfig       `json:"origins" yaml:"origins"`
	Notifications      []NotificationConfig `json:"notifications" yaml:"notifications"`
}

func decodeConfig(path string, data []byte) (rawConfig, error) {
	var tmpConfig rawConfig

	// Determine file format based on file name extension
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &tmpConfig); err != nil {
			return rawConfig{}, fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &tmpConfig); err != nil {
			return rawConfig{}, fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		// Default to JSON for backward compatibility
		if err := json.Unmarshal(data, &tmpConfig); err != nil {
			return rawConfig{}, fmt.Errorf("failed to parse config (assumed JSON): %w", err)
		}
	}

	return tmpConfig, nil
}

func buildConfig(tmpConfig rawConfig) *Config {
	return &Config{
		CloudflareAPIToken: tmpConfig.CloudflareAPIToken,
		CloudflareZoneIDs:  tmpConfig.CloudflareZoneIDs,
		CheckInterval:      time.Duration(tmpConfig.CheckInterval) * time.Second,
		Origins:            tmpConfig.Origins,
		Notifications:      tmpConfig.Notifications,
	}
}

func applyLegacyZoneConfig(config *Config, tmpConfig rawConfig) {
	if tmpConfig.CloudflareZoneID == "" || len(tmpConfig.CloudflareZoneIDs) > 0 {
		return
	}

	config.CloudflareZoneIDs = []ZoneConfig{
		{
			ZoneID: tmpConfig.CloudflareZoneID,
			Name:   "default", // デフォルト名
		},
	}

	applyDefaultZoneName(config.Origins, "default")
}

func applyDefaultZoneName(origins []OriginConfig, zoneName string) {
	for i := range origins {
		if origins[i].ZoneName == "" {
			origins[i].ZoneName = zoneName
		}
	}
}

func normalizeOrigins(config *Config) error {
	defaultZoneName := ""
	if len(config.CloudflareZoneIDs) == 1 {
		defaultZoneName = config.CloudflareZoneIDs[0].Name
	}

	for i := range config.Origins {
		origin := &config.Origins[i]
		normalizeOriginPriorityLevels(origin)
		if err := validateRecordType(origin.RecordType); err != nil {
			return fmt.Errorf("invalid record type for origin %s: %w", origin.Name, err)
		}
		if origin.ZoneName == "" && defaultZoneName != "" {
			origin.ZoneName = defaultZoneName
		}
	}
	return nil
}

func normalizeOriginPriorityLevels(origin *OriginConfig) {
	origin.PriorityLevels = NormalizePriorityLevels(origin.PriorityLevels)
	if len(origin.PriorityLevels) > 0 {
		return
	}
	origin.PriorityLevels = NormalizePriorityLevels(
		legacyPriorityLevels(origin.PriorityFailoverIPs, origin.FailoverIPs),
	)
}

func validateRecordType(recordType string) error {
	if recordType == "A" || recordType == "AAAA" {
		return nil
	}
	return fmt.Errorf("unsupported record type: %s", recordType)
}
