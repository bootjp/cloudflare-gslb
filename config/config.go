package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config はアプリケーションの設定を表す構造体
type Config struct {
	CloudflareAPIToken string               `json:"cloudflare_api_token"`
	CloudflareZoneIDs  []ZoneConfig         `json:"cloudflare_zones"`
	CheckInterval      time.Duration        `json:"check_interval_seconds"`
	Origins            []OriginConfig       `json:"origins"`
	Notifications      []NotificationConfig `json:"notifications"` // 通知設定
}

// ZoneConfig はCloudflareゾーンの設定を表す構造体
type ZoneConfig struct {
	ZoneID string `json:"zone_id"`
	Name   string `json:"name"`
}

// OriginConfig はオリジンサーバーの設定を表す構造体
type OriginConfig struct {
	Name                string          `json:"name"`
	ZoneName            string          `json:"zone_name"`   // 対象のゾーン名
	RecordType          string          `json:"record_type"` // "A" または "AAAA"
	HealthCheck         HealthCheck     `json:"health_check"`
	PriorityLevels      []PriorityLevel `json:"priority_levels,omitempty"`       // 優先度付きIPグループ（高い値ほど優先）
	PriorityFailoverIPs []string        `json:"priority_failover_ips,omitempty"` // 互換用: 優先的に使用するフェイルオーバー用のIPアドレスリスト
	FailoverIPs         []string        `json:"failover_ips,omitempty"`          // 互換用: フェイルオーバー用のIPアドレスリスト
	Proxied             bool            `json:"proxied"`                         // Cloudflareのプロキシを有効にするかどうか
	ReturnToPriority    bool            `json:"return_to_priority"`              // 正常に戻ったときに優先IPに戻すかどうか
}

// PriorityLevel は優先度付きIPグループを表す構造体
type PriorityLevel struct {
	Priority int      `json:"priority"`
	IPs      []string `json:"ips"`
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
	Type               string            `json:"type"`                 // "http", "https", "icmp"
	Endpoint           string            `json:"endpoint"`             // HTTPSの場合のパス
	Host               string            `json:"host"`                 // HTTPSの場合のホスト名
	Timeout            int               `json:"timeout"`              // タイムアウト（秒）
	InsecureSkipVerify bool              `json:"insecure_skip_verify"` // HTTPSの場合に証明書検証をスキップするかどうか
	Headers            map[string]string `json:"headers"`              // ヘルスチェックリクエストに追加するHTTPヘッダ
}

// NotificationConfig は通知設定を表す構造体
type NotificationConfig struct {
	Type       string `json:"type"`        // "slack" または "discord"
	WebhookURL string `json:"webhook_url"` // WebhookのURL
}

// LoadConfig は設定ファイルを読み込む関数
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	tmpConfig, err := decodeConfig(file)
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
	CloudflareAPIToken string               `json:"cloudflare_api_token"`
	CloudflareZoneID   string               `json:"cloudflare_zone_id"`
	CloudflareZoneIDs  []ZoneConfig         `json:"cloudflare_zones"`
	CheckInterval      int                  `json:"check_interval_seconds"`
	Origins            []OriginConfig       `json:"origins"`
	Notifications      []NotificationConfig `json:"notifications"`
}

func decodeConfig(file *os.File) (rawConfig, error) {
	var tmpConfig rawConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&tmpConfig); err != nil {
		return rawConfig{}, err
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
