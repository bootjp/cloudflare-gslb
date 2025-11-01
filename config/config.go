package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config はアプリケーションの設定を表す構造体
type Config struct {
	CloudflareAPIToken string         `json:"cloudflare_api_token" yaml:"cloudflare_api_token"`
	CloudflareZoneIDs  []ZoneConfig   `json:"cloudflare_zones" yaml:"cloudflare_zones"`
	CheckInterval      time.Duration  `json:"check_interval_seconds" yaml:"check_interval_seconds"`
	Origins            []OriginConfig `json:"origins" yaml:"origins"`
}

// ZoneConfig はCloudflareゾーンの設定を表す構造体
type ZoneConfig struct {
	ZoneID string `json:"zone_id" yaml:"zone_id"`
	Name   string `json:"name" yaml:"name"`
}

// OriginConfig はオリジンサーバーの設定を表す構造体
type OriginConfig struct {
	Name                string      `json:"name" yaml:"name"`
	ZoneName            string      `json:"zone_name" yaml:"zone_name"`     // 対象のゾーン名
	RecordType          string      `json:"record_type" yaml:"record_type"` // "A" または "AAAA"
	HealthCheck         HealthCheck `json:"health_check" yaml:"health_check"`
	PriorityFailoverIPs []string    `json:"priority_failover_ips" yaml:"priority_failover_ips"` // 優先的に使用するフェイルオーバー用のIPアドレスリスト
	FailoverIPs         []string    `json:"failover_ips" yaml:"failover_ips"`                   // フェイルオーバー用のIPアドレスリスト
	Proxied             bool        `json:"proxied" yaml:"proxied"`                             // Cloudflareのプロキシを有効にするかどうか
	ReturnToPriority    bool        `json:"return_to_priority" yaml:"return_to_priority"`       // 正常に戻ったときに優先IPに戻すかどうか
}

// HealthCheck はヘルスチェックの設定を表す構造体
type HealthCheck struct {
	Type               string `json:"type" yaml:"type"`                                 // "http", "https", "icmp"
	Endpoint           string `json:"endpoint" yaml:"endpoint"`                         // HTTPSの場合のパス
	Host               string `json:"host" yaml:"host"`                                 // HTTPSの場合のホスト名
	Timeout            int    `json:"timeout" yaml:"timeout"`                           // タイムアウト（秒）
	InsecureSkipVerify bool   `json:"insecure_skip_verify" yaml:"insecure_skip_verify"` // HTTPSの場合に証明書検証をスキップするかどうか
}

// LoadConfig は設定ファイルを読み込む関数
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tmpConfig struct {
		CloudflareAPIToken string         `json:"cloudflare_api_token" yaml:"cloudflare_api_token"`
		CloudflareZoneID   string         `json:"cloudflare_zone_id" yaml:"cloudflare_zone_id"`
		CloudflareZoneIDs  []ZoneConfig   `json:"cloudflare_zones" yaml:"cloudflare_zones"`
		CheckInterval      int            `json:"check_interval_seconds" yaml:"check_interval_seconds"`
		Origins            []OriginConfig `json:"origins" yaml:"origins"`
	}

	if err := json.Unmarshal(data, &tmpConfig); err != nil {
		if yamlErr := unmarshalYAMLConfig(data, &tmpConfig); yamlErr != nil {
			return nil, fmt.Errorf("failed to parse config as JSON (%v) or YAML (%v)", err, yamlErr)
		}
	}

	// 設定の初期化
	config := &Config{
		CloudflareAPIToken: tmpConfig.CloudflareAPIToken,
		CloudflareZoneIDs:  tmpConfig.CloudflareZoneIDs,
		CheckInterval:      time.Duration(tmpConfig.CheckInterval) * time.Second,
		Origins:            tmpConfig.Origins,
	}

	// 後方互換性のために単一のZoneIDから変換
	if tmpConfig.CloudflareZoneID != "" && len(tmpConfig.CloudflareZoneIDs) == 0 {
		config.CloudflareZoneIDs = []ZoneConfig{
			{
				ZoneID: tmpConfig.CloudflareZoneID,
				Name:   "default", // デフォルト名
			},
		}

		// 各オリジンに対して、ゾーン名が指定されていない場合はデフォルトゾーンを使用
		for i := range config.Origins {
			if config.Origins[i].ZoneName == "" {
				config.Origins[i].ZoneName = "default"
			}
		}
	}

	return config, nil
}
