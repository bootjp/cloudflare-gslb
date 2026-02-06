package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "https",
					"endpoint": "/health",
					"host": "example.com",
					"timeout": 5,
					"headers": {
						"X-Test-Header": "header-value"
					}
				}
			},
			{
				"name": "api.example.com",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/status",
					"host": "api.example.com",
					"timeout": 5
				}
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "config_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testConfigContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.CloudflareAPIToken != "test-token" {
		t.Errorf("Expected CloudflareAPIToken = 'test-token', got '%s'", config.CloudflareAPIToken)
	}
	if len(config.CloudflareZoneIDs) != 1 {
		t.Errorf("Expected 1 zone ID, got %d", len(config.CloudflareZoneIDs))
	}
	if config.CloudflareZoneIDs[0].ZoneID != "test-zone" {
		t.Errorf("Expected CloudflareZoneIDs[0].ZoneID = 'test-zone', got '%s'", config.CloudflareZoneIDs[0].ZoneID)
	}
	if config.CloudflareZoneIDs[0].Name != "default" {
		t.Errorf("Expected CloudflareZoneIDs[0].Name = 'default', got '%s'", config.CloudflareZoneIDs[0].Name)
	}
	if config.CheckInterval != 60*time.Second {
		t.Errorf("Expected CheckInterval = 60s, got %v", config.CheckInterval)
	}

	if len(config.Origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(config.Origins))
	}

	if config.Origins[0].Name != "example.com" {
		t.Errorf("Expected first origin name = 'example.com', got '%s'", config.Origins[0].Name)
	}
	if config.Origins[0].RecordType != "A" {
		t.Errorf("Expected first origin record type = 'A', got '%s'", config.Origins[0].RecordType)
	}
	if config.Origins[0].HealthCheck.Type != "https" {
		t.Errorf("Expected first origin health check type = 'https', got '%s'", config.Origins[0].HealthCheck.Type)
	}
	if config.Origins[0].HealthCheck.Endpoint != "/health" {
		t.Errorf("Expected first origin health check endpoint = '/health', got '%s'", config.Origins[0].HealthCheck.Endpoint)
	}
	if config.Origins[0].HealthCheck.Host != "example.com" {
		t.Errorf("Expected first origin health check host = 'example.com', got '%s'", config.Origins[0].HealthCheck.Host)
	}
	if config.Origins[0].HealthCheck.Timeout != 5 {
		t.Errorf("Expected first origin health check timeout = 5, got %d", config.Origins[0].HealthCheck.Timeout)
	}
	if config.Origins[0].HealthCheck.Headers == nil {
		t.Errorf("Expected first origin health check headers to be initialized")
	} else if headerValue := config.Origins[0].HealthCheck.Headers["X-Test-Header"]; headerValue != "header-value" {
		t.Errorf("Expected first origin health check header X-Test-Header = 'header-value', got '%s'", headerValue)
	}
	if config.Origins[0].ZoneName != "default" {
		t.Errorf("Expected first origin zone name = 'default', got '%s'", config.Origins[0].ZoneName)
	}

	if config.Origins[1].Name != "api.example.com" {
		t.Errorf("Expected second origin name = 'api.example.com', got '%s'", config.Origins[1].Name)
	}
	if config.Origins[1].RecordType != "A" {
		t.Errorf("Expected second origin record type = 'A', got '%s'", config.Origins[1].RecordType)
	}
	if config.Origins[1].HealthCheck.Type != "http" {
		t.Errorf("Expected second origin health check type = 'http', got '%s'", config.Origins[1].HealthCheck.Type)
	}
	if config.Origins[1].ZoneName != "default" {
		t.Errorf("Expected second origin zone name = 'default', got '%s'", config.Origins[1].ZoneName)
	}
}

func TestLoadConfig_Error(t *testing.T) {
	_, err := LoadConfig("nonexistent_file.json")
	if err == nil {
		t.Errorf("LoadConfig() expected error for nonexistent file, got nil")
	}

	invalidJSON := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "https",
					"endpoint": "/health",
					"host": "example.com",
					"timeout": 5
				}
			},
		]
	}`

	tmpfile, err := os.CreateTemp("", "invalid_config_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(invalidJSON)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Errorf("LoadConfig() expected error for invalid JSON, got nil")
	}
}

func TestLoadMultiZoneConfig(t *testing.T) {
	testMultiZoneConfigContent := `{
		"cloudflare_api_token": "test-token",
		"check_interval_seconds": 60,
		"cloudflare_zones": [
			{
				"zone_id": "zone-1",
				"name": "example.com"
			},
			{
				"zone_id": "zone-2",
				"name": "example.org"
			}
		],
		"origins": [
			{
				"name": "www",
				"zone_name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "https",
					"endpoint": "/health",
					"host": "www.example.com",
					"timeout": 5
				}
			},
			{
				"name": "api",
				"zone_name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/status",
					"host": "api.example.com",
					"timeout": 5
				}
			},
			{
				"name": "ipv6",
				"zone_name": "example.org",
				"record_type": "AAAA",
				"health_check": {
					"type": "icmp",
					"timeout": 5
				}
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "multizone_config_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testMultiZoneConfigContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.CloudflareAPIToken != "test-token" {
		t.Errorf("Expected CloudflareAPIToken = 'test-token', got '%s'", config.CloudflareAPIToken)
	}

	if len(config.CloudflareZoneIDs) != 2 {
		t.Errorf("Expected 2 zone IDs, got %d", len(config.CloudflareZoneIDs))
	}

	if config.CloudflareZoneIDs[0].ZoneID != "zone-1" {
		t.Errorf("Expected CloudflareZoneIDs[0].ZoneID = 'zone-1', got '%s'", config.CloudflareZoneIDs[0].ZoneID)
	}
	if config.CloudflareZoneIDs[0].Name != "example.com" {
		t.Errorf("Expected CloudflareZoneIDs[0].Name = 'example.com', got '%s'", config.CloudflareZoneIDs[0].Name)
	}

	if config.CloudflareZoneIDs[1].ZoneID != "zone-2" {
		t.Errorf("Expected CloudflareZoneIDs[1].ZoneID = 'zone-2', got '%s'", config.CloudflareZoneIDs[1].ZoneID)
	}
	if config.CloudflareZoneIDs[1].Name != "example.org" {
		t.Errorf("Expected CloudflareZoneIDs[1].Name = 'example.org', got '%s'", config.CloudflareZoneIDs[1].Name)
	}

	if config.CheckInterval != 60*time.Second {
		t.Errorf("Expected CheckInterval = 60s, got %v", config.CheckInterval)
	}

	if len(config.Origins) != 3 {
		t.Errorf("Expected 3 origins, got %d", len(config.Origins))
	}

	if config.Origins[0].Name != "www" {
		t.Errorf("Expected first origin name = 'www', got '%s'", config.Origins[0].Name)
	}
	if config.Origins[0].ZoneName != "example.com" {
		t.Errorf("Expected first origin zone name = 'example.com', got '%s'", config.Origins[0].ZoneName)
	}
	if config.Origins[0].RecordType != "A" {
		t.Errorf("Expected first origin record type = 'A', got '%s'", config.Origins[0].RecordType)
	}

	if config.Origins[1].Name != "api" {
		t.Errorf("Expected second origin name = 'api', got '%s'", config.Origins[1].Name)
	}
	if config.Origins[1].ZoneName != "example.com" {
		t.Errorf("Expected second origin zone name = 'example.com', got '%s'", config.Origins[1].ZoneName)
	}

	if config.Origins[2].Name != "ipv6" {
		t.Errorf("Expected third origin name = 'ipv6', got '%s'", config.Origins[2].Name)
	}
	if config.Origins[2].ZoneName != "example.org" {
		t.Errorf("Expected third origin zone name = 'example.org', got '%s'", config.Origins[2].ZoneName)
	}
	if config.Origins[2].RecordType != "AAAA" {
		t.Errorf("Expected third origin record type = 'AAAA', got '%s'", config.Origins[2].RecordType)
	}
}

func TestInvalidConfig(t *testing.T) {
	// 不正なJSONを含む設定ファイル（終わりの括弧が足りない）
	invalidJSON := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "https",
					"endpoint": "/health",
					"host": "example.com",
					"timeout": 5
				}
			},
			{
				"name": "api.example.com",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/status",
					"host": "api.example.com",
					"timeout": 5
				}
			}
		]
	` // 終わりの括弧が足りない

	tmpfile, err := os.CreateTemp("", "invalid_config_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(invalidJSON)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// 不正なJSONファイルを読み込む
	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Errorf("LoadConfig() expected error for invalid JSON, got nil")
	}
}

func TestNonExistentZoneInConfig(t *testing.T) {
	invalidZoneConfigContent := `{
		"cloudflare_api_token": "test-token",
		"check_interval_seconds": 60,
		"cloudflare_zones": [
			{
				"zone_id": "zone-1",
				"name": "example.com"
			}
		],
		"origins": [
			{
				"name": "www",
				"zone_name": "example.com",
				"record_type": "A",
				"health_check": {
					"type": "https",
					"endpoint": "/health",
					"host": "www.example.com",
					"timeout": 5
				}
			},
			{
				"name": "api",
				"zone_name": "non-existent-zone",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/status",
					"host": "api.example.com",
					"timeout": 5
				}
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "invalid_zone_config_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(invalidZoneConfigContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if len(config.CloudflareZoneIDs) != 1 {
		t.Errorf("Expected 1 zone ID, got %d", len(config.CloudflareZoneIDs))
	}
	if len(config.Origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(config.Origins))
	}
}

func TestLoadConfig_LegacyPriorityLevels(t *testing.T) {
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "example.com",
				"record_type": "A",
				"priority_failover_ips": ["192.168.1.1", "192.168.1.2"],
				"failover_ips": ["192.168.1.3"]
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "legacy_priority_config_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testConfigContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if len(cfg.Origins) != 1 {
		t.Fatalf("Expected 1 origin, got %d", len(cfg.Origins))
	}

	levels := cfg.Origins[0].PriorityLevels
	if len(levels) != 2 {
		t.Fatalf("Expected 2 priority levels, got %d", len(levels))
	}

	if levels[0].Priority != LegacyPriorityHigh {
		t.Errorf("Expected high priority %d, got %d", LegacyPriorityHigh, levels[0].Priority)
	}
	if levels[1].Priority != LegacyPriorityLow {
		t.Errorf("Expected low priority %d, got %d", LegacyPriorityLow, levels[1].Priority)
	}
	if len(levels[0].IPs) != 2 || levels[0].IPs[0] != "192.168.1.1" {
		t.Errorf("Unexpected high priority IPs: %v", levels[0].IPs)
	}
	if len(levels[1].IPs) != 1 || levels[1].IPs[0] != "192.168.1.3" {
		t.Errorf("Unexpected low priority IPs: %v", levels[1].IPs)
	}
}

func TestLoadConfig_InvalidRecordType(t *testing.T) {
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "example.com",
				"record_type": "CNAME",
				"priority_failover_ips": ["192.168.1.1"]
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "invalid_record_type_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testConfigContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Fatalf("LoadConfig() expected error for invalid record type, got nil")
	}
}

func TestLoadYAMLConfig(t *testing.T) {
	testYAMLContent := `cloudflare_api_token: test-token
cloudflare_zone_id: test-zone
check_interval_seconds: 60
origins:
  - name: example.com
    record_type: A
    health_check:
      type: https
      endpoint: /health
      host: example.com
      timeout: 5
      headers:
        X-Test-Header: header-value
  - name: api.example.com
    record_type: A
    health_check:
      type: http
      endpoint: /status
      host: api.example.com
      timeout: 5
`

	tmpfile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testYAMLContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.CloudflareAPIToken != "test-token" {
		t.Errorf("Expected CloudflareAPIToken = 'test-token', got '%s'", config.CloudflareAPIToken)
	}
	if len(config.CloudflareZoneIDs) != 1 {
		t.Errorf("Expected 1 zone ID, got %d", len(config.CloudflareZoneIDs))
	}
	if config.CloudflareZoneIDs[0].ZoneID != "test-zone" {
		t.Errorf("Expected CloudflareZoneIDs[0].ZoneID = 'test-zone', got '%s'", config.CloudflareZoneIDs[0].ZoneID)
	}
	if config.CheckInterval != 60*time.Second {
		t.Errorf("Expected CheckInterval = 60s, got %v", config.CheckInterval)
	}

	if len(config.Origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(config.Origins))
	}

	if config.Origins[0].Name != "example.com" {
		t.Errorf("Expected first origin name = 'example.com', got '%s'", config.Origins[0].Name)
	}
	if config.Origins[0].HealthCheck.Headers == nil {
		t.Errorf("Expected first origin health check headers to be initialized")
	} else if headerValue := config.Origins[0].HealthCheck.Headers["X-Test-Header"]; headerValue != "header-value" {
		t.Errorf("Expected first origin health check header X-Test-Header = 'header-value', got '%s'", headerValue)
	}
}

func TestLoadMultiZoneYAMLConfig(t *testing.T) {
	testYAMLContent := `cloudflare_api_token: test-token
check_interval_seconds: 60
cloudflare_zones:
  - zone_id: zone-1
    name: example.com
  - zone_id: zone-2
    name: example.org
notifications:
  - type: slack
    webhook_url: https://hooks.slack.com/services/test
  - type: discord
    webhook_url: https://discord.com/api/webhooks/test
origins:
  - name: www
    zone_name: example.com
    record_type: A
    health_check:
      type: https
      endpoint: /health
      host: www.example.com
      timeout: 5
    priority_levels:
      - priority: 100
        ips:
          - 192.168.1.1
          - 192.168.1.2
      - priority: 50
        ips:
          - 192.168.1.3
    proxied: true
    return_to_priority: true
  - name: ipv6
    zone_name: example.org
    record_type: AAAA
    health_check:
      type: icmp
      timeout: 5
    priority_levels:
      - priority: 100
        ips:
          - "2001:db8::1"
    proxied: false
`

	tmpfile, err := os.CreateTemp("", "multizone_yaml_*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(testYAMLContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.CloudflareAPIToken != "test-token" {
		t.Errorf("Expected CloudflareAPIToken = 'test-token', got '%s'", config.CloudflareAPIToken)
	}

	if len(config.CloudflareZoneIDs) != 2 {
		t.Errorf("Expected 2 zone IDs, got %d", len(config.CloudflareZoneIDs))
	}

	if config.CloudflareZoneIDs[0].ZoneID != "zone-1" || config.CloudflareZoneIDs[0].Name != "example.com" {
		t.Errorf("Unexpected zone config: %+v", config.CloudflareZoneIDs[0])
	}

	if len(config.Notifications) != 2 {
		t.Errorf("Expected 2 notifications, got %d", len(config.Notifications))
	}

	if config.Notifications[0].Type != "slack" {
		t.Errorf("Expected first notification type = 'slack', got '%s'", config.Notifications[0].Type)
	}

	if len(config.Origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(config.Origins))
	}

	if config.Origins[0].Name != "www" {
		t.Errorf("Expected first origin name = 'www', got '%s'", config.Origins[0].Name)
	}

	if len(config.Origins[0].PriorityLevels) != 2 {
		t.Errorf("Expected 2 priority levels, got %d", len(config.Origins[0].PriorityLevels))
	}

	if config.Origins[0].PriorityLevels[0].Priority != 100 {
		t.Errorf("Expected first priority level = 100, got %d", config.Origins[0].PriorityLevels[0].Priority)
	}

	if len(config.Origins[0].PriorityLevels[0].IPs) != 2 {
		t.Errorf("Expected 2 IPs in first priority level, got %d", len(config.Origins[0].PriorityLevels[0].IPs))
	}

	if config.Origins[0].Proxied != true {
		t.Errorf("Expected first origin proxied = true, got %v", config.Origins[0].Proxied)
	}

	if config.Origins[1].RecordType != "AAAA" {
		t.Errorf("Expected second origin record type = 'AAAA', got '%s'", config.Origins[1].RecordType)
	}
}

func TestLoadYAMLConfig_InvalidYAML(t *testing.T) {
	invalidYAML := `cloudflare_api_token: test-token
check_interval_seconds: 60
origins:
  - name: example.com
    record_type: A
    health_check:
      type: https
      endpoint: /health
      host: "unclosed quote
      timeout: 5
`

	tmpfile, err := os.CreateTemp("", "invalid_yaml_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(invalidYAML)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Errorf("LoadConfig() expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_DirectoryWithConfigFile(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "config_dir_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with config.yaml
	yamlContent := `cloudflare_api_token: test-token-yaml
check_interval_seconds: 30
cloudflare_zones:
  - zone_id: test-zone
    name: example.com
origins:
  - name: www
    zone_name: example.com
    record_type: A
    health_check:
      type: https
      endpoint: /health
      host: www.example.com
      timeout: 5
    priority_levels:
      - priority: 100
        ips: [192.168.1.1]
`
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}

	// Load config from directory (should find config.yaml)
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig() with directory failed: %v", err)
	}

	if config.CloudflareAPIToken != "test-token-yaml" {
		t.Errorf("Expected CloudflareAPIToken = 'test-token-yaml', got '%s'", config.CloudflareAPIToken)
	}
	if config.CheckInterval != 30*time.Second {
		t.Errorf("Expected CheckInterval = 30s, got %v", config.CheckInterval)
	}
}

func TestLoadConfig_DirectoryWithMultipleConfigFiles(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "config_dir_multi_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create both YAML and JSON configs to test precedence order
	// YAML should be preferred over JSON
	yamlContent := `cloudflare_api_token: yaml-token
check_interval_seconds: 30
cloudflare_zones:
  - zone_id: test-zone
    name: example.com
`
	// JSON config with different token to verify YAML takes precedence
	jsonContent := `{
		"cloudflare_api_token": "json-token",
		"check_interval_seconds": 60,
		"cloudflare_zones": [{"zone_id": "test-zone", "name": "example.com"}]
	}`

	yamlPath := filepath.Join(tmpDir, "config.yaml")
	jsonPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}

	// Load config from directory (should prefer config.yaml)
	config, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig() with directory failed: %v", err)
	}

	if config.CloudflareAPIToken != "yaml-token" {
		t.Errorf("Expected YAML to be preferred, got token '%s'", config.CloudflareAPIToken)
	}
}

func TestLoadConfig_DirectoryWithoutConfigFile(t *testing.T) {
	// Create a temporary directory with no config files
	tmpDir, err := os.MkdirTemp("", "config_dir_empty_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Try to load config from empty directory
	_, err = LoadConfig(tmpDir)
	if err == nil {
		t.Errorf("LoadConfig() expected error for directory without config, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "no config file found") {
		t.Errorf("Expected 'no config file found' error, got: %v", err)
	}
}
