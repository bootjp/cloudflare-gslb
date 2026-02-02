package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// SingleRecordTypes は複数のレコードを設定できないレコードタイプのリスト（RFC準拠）
var SingleRecordTypes = map[string]bool{
	"CNAME": true, // CNAMEレコードは同じ名前に複数設定できない
	"SOA":   true, // SOAレコードは1つだけ
}

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

// PriorityIP は優先IPアドレスとその優先度を表す構造体
type PriorityIP struct {
	IP       string `json:"ip"`       // IPアドレス
	Priority int    `json:"priority"` // 優先度（大きいほど優先）
}

// OriginConfig はオリジンサーバーの設定を表す構造体
type OriginConfig struct {
	Name                string       `json:"name"`
	ZoneName            string       `json:"zone_name"`   // 対象のゾーン名
	RecordType          string       `json:"record_type"` // "A" または "AAAA"
	HealthCheck         HealthCheck  `json:"health_check"`
	PriorityFailoverIPs []PriorityIP `json:"priority_failover_ips"` // 優先的に使用するフェイルオーバー用のIPアドレスリスト
	FailoverIPs         []string     `json:"failover_ips"`          // フェイルオーバー用のIPアドレスリスト
	Proxied             bool         `json:"proxied"`               // Cloudflareのプロキシを有効にするかどうか
	ReturnToPriority    bool         `json:"return_to_priority"`    // 正常に戻ったときに優先IPに戻すかどうか
}

// GetPriorityIPs は優先度順にソートされたIPアドレスのリストを返す（優先度が大きいほど優先）
func (o *OriginConfig) GetPriorityIPs() []string {
	if len(o.PriorityFailoverIPs) == 0 {
		return nil
	}

	// 優先度でソートしたコピーを作成（降順：大きい値が先）
	sorted := make([]PriorityIP, len(o.PriorityFailoverIPs))
	copy(sorted, o.PriorityFailoverIPs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	// IPアドレスのみを返す
	ips := make([]string, len(sorted))
	for i, p := range sorted {
		ips[i] = p.IP
	}
	return ips
}

// GetPriorityIPsByPriority は指定された優先度を持つすべてのIPアドレスを返す
func (o *OriginConfig) GetPriorityIPsByPriority(priority int) []string {
	var ips []string
	for _, p := range o.PriorityFailoverIPs {
		if p.Priority == priority {
			ips = append(ips, p.IP)
		}
	}
	return ips
}

// GetHighestPriority は最も高い優先度（最も大きい優先度値）を返す
func (o *OriginConfig) GetHighestPriority() (int, bool) {
	if len(o.PriorityFailoverIPs) == 0 {
		return 0, false
	}

	maxPriority := o.PriorityFailoverIPs[0].Priority
	for _, p := range o.PriorityFailoverIPs[1:] {
		if p.Priority > maxPriority {
			maxPriority = p.Priority
		}
	}
	return maxPriority, true
}

// IsPriorityIP は指定されたIPが優先IPかどうかを返す
func (o *OriginConfig) IsPriorityIP(ip string) bool {
	for _, priorityIP := range o.PriorityFailoverIPs {
		if priorityIP.IP == ip {
			return true
		}
	}
	return false
}

// IsSingleRecordType は複数のレコードを設定できないレコードタイプかどうかを返す
func (o *OriginConfig) IsSingleRecordType() bool {
	return SingleRecordTypes[o.RecordType]
}

// ValidateMultipleRecords は同一優先度で複数のIPが設定されている場合にレコードタイプをチェックする
func (o *OriginConfig) ValidateMultipleRecords() error {
	if !o.IsSingleRecordType() {
		return nil
	}

	// 各優先度でIPの数をカウント
	priorityCounts := make(map[int]int)
	for _, p := range o.PriorityFailoverIPs {
		priorityCounts[p.Priority]++
	}

	// 同一優先度で複数のIPが設定されている場合はエラー
	for priority, count := range priorityCounts {
		if count > 1 {
			return fmt.Errorf("record type %s cannot have multiple records with the same priority %d (RFC violation)", o.RecordType, priority)
		}
	}

	return nil
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

// rawOriginConfig は設定ファイルからの読み込み用の中間構造体
type rawOriginConfig struct {
	Name                string          `json:"name"`
	ZoneName            string          `json:"zone_name"`
	RecordType          string          `json:"record_type"`
	HealthCheck         HealthCheck     `json:"health_check"`
	PriorityFailoverIPs json.RawMessage `json:"priority_failover_ips"` // 文字列配列またはPriorityIP配列
	FailoverIPs         []string        `json:"failover_ips"`
	Proxied             bool            `json:"proxied"`
	ReturnToPriority    bool            `json:"return_to_priority"`
}

// parsePriorityFailoverIPs は priority_failover_ips の後方互換性をサポートするパーサー
func parsePriorityFailoverIPs(raw json.RawMessage) ([]PriorityIP, error) {
	if raw == nil || len(raw) == 0 {
		return nil, nil
	}

	// まず新しい形式（PriorityIP配列）でパースを試みる
	var priorityIPs []PriorityIP
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
			return priorityIPs, nil
		}
	}

	// 古い形式（文字列配列）でパースを試みる
	var ips []string
	if err := json.Unmarshal(raw, &ips); err != nil {
		return nil, err
	}

	// 文字列配列をPriorityIP配列に変換（インデックス順に優先度を設定）
	priorityIPs = make([]PriorityIP, len(ips))
	for i, ip := range ips {
		priorityIPs[i] = PriorityIP{
			IP:       ip,
			Priority: i, // 配列のインデックスを優先度として使用
		}
	}

	return priorityIPs, nil
}

// LoadConfig は設定ファイルを読み込む関数
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var tmpConfig struct {
		CloudflareAPIToken string               `json:"cloudflare_api_token"`
		CloudflareZoneID   string               `json:"cloudflare_zone_id"`
		CloudflareZoneIDs  []ZoneConfig         `json:"cloudflare_zones"`
		CheckInterval      int                  `json:"check_interval_seconds"`
		Origins            []rawOriginConfig    `json:"origins"`
		Notifications      []NotificationConfig `json:"notifications"`
	}

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&tmpConfig); err != nil {
		return nil, err
	}

	// オリジン設定を変換
	origins := make([]OriginConfig, len(tmpConfig.Origins))
	for i, rawOrigin := range tmpConfig.Origins {
		priorityIPs, err := parsePriorityFailoverIPs(rawOrigin.PriorityFailoverIPs)
		if err != nil {
			return nil, err
		}

		origins[i] = OriginConfig{
			Name:                rawOrigin.Name,
			ZoneName:            rawOrigin.ZoneName,
			RecordType:          rawOrigin.RecordType,
			HealthCheck:         rawOrigin.HealthCheck,
			PriorityFailoverIPs: priorityIPs,
			FailoverIPs:         rawOrigin.FailoverIPs,
			Proxied:             rawOrigin.Proxied,
			ReturnToPriority:    rawOrigin.ReturnToPriority,
		}

		// 複数レコードの検証（RFC準拠チェック）
		if err := origins[i].ValidateMultipleRecords(); err != nil {
			return nil, fmt.Errorf("origin %s: %w", rawOrigin.Name, err)
		}
	}

	// 設定の初期化
	config := &Config{
		CloudflareAPIToken: tmpConfig.CloudflareAPIToken,
		CloudflareZoneIDs:  tmpConfig.CloudflareZoneIDs,
		CheckInterval:      time.Duration(tmpConfig.CheckInterval) * time.Second,
		Origins:            origins,
		Notifications:      tmpConfig.Notifications,
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
