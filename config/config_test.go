package config

import (
	"os"
	"testing"
	"time"
)

func createTempConfigFile(t *testing.T, pattern, content string) string {
	t.Helper()

	tmpfile, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	t.Cleanup(func() {
		os.Remove(tmpfile.Name())
	})

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	return tmpfile.Name()
}

func loadConfigFromContent(t *testing.T, pattern, content string) *Config {
	t.Helper()

	path := createTempConfigFile(t, pattern, content)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	return config
}

func assertSingleZoneConfig(t *testing.T, config *Config) {
	t.Helper()

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
        }`

	config := loadConfigFromContent(t, "config_test_*.json", testConfigContent)
	assertSingleZoneConfig(t, config)
}

func TestLoadConfigYAML(t *testing.T) {
	testConfigContent := `
cloudflare_api_token: test-token
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
  - name: api.example.com
    record_type: A
    health_check:
      type: http
      endpoint: /status
      host: api.example.com
      timeout: 5
`

	config := loadConfigFromContent(t, "config_test_*.yaml", testConfigContent)
	assertSingleZoneConfig(t, config)
}

func TestLoadConfigYAMLWithEmptyAndInlineCollections(t *testing.T) {
	testConfigContent := `
cloudflare_api_token: another-token
cloudflare_zones:
  - zone_id: zone-42
    name: zone.example
check_interval_seconds: 45
origins:
  - name: app
    zone_name: zone.example
    record_type: A
    health_check:
      type: http
      timeout: 10
    priority_failover_ips: []
    failover_ips: ["192.0.2.10", "192.0.2.11"]
    proxied: true
    return_to_priority: false
`

	config := loadConfigFromContent(t, "config_empty_inline_test_*.yaml", testConfigContent)

	if config.CloudflareAPIToken != "another-token" {
		t.Fatalf("Expected CloudflareAPIToken = 'another-token', got '%s'", config.CloudflareAPIToken)
	}
	if len(config.CloudflareZoneIDs) != 1 {
		t.Fatalf("Expected 1 zone, got %d", len(config.CloudflareZoneIDs))
	}
	if len(config.Origins) != 1 {
		t.Fatalf("Expected 1 origin, got %d", len(config.Origins))
	}

	origin := config.Origins[0]
	if len(origin.PriorityFailoverIPs) != 0 {
		t.Fatalf("Expected empty PriorityFailoverIPs, got %d entries", len(origin.PriorityFailoverIPs))
	}
	if len(origin.FailoverIPs) != 2 {
		t.Fatalf("Expected 2 FailoverIPs, got %d", len(origin.FailoverIPs))
	}
	if origin.FailoverIPs[0] != "192.0.2.10" || origin.FailoverIPs[1] != "192.0.2.11" {
		t.Fatalf("Unexpected FailoverIPs: %#v", origin.FailoverIPs)
	}
	if !origin.Proxied {
		t.Fatalf("Expected Proxied to be true")
	}
	if origin.ReturnToPriority {
		t.Fatalf("Expected ReturnToPriority to be false")
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

	path := createTempConfigFile(t, "invalid_config_*.json", invalidJSON)

	_, err = LoadConfig(path)
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

	config := loadConfigFromContent(t, "multizone_config_test_*.json", testMultiZoneConfigContent)

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

func TestLoadMultiZoneConfigYAML(t *testing.T) {
	testConfigContent := `
cloudflare_api_token: test-token
check_interval_seconds: 120
cloudflare_zones:
  - zone_id: zone-1
    name: example.com
  - zone_id: zone-2
    name: example.org
origins:
  - name: www
    zone_name: example.com
    record_type: A
    health_check:
      type: https
      endpoint: /health
      host: www.example.com
      timeout: 5
    priority_failover_ips:
      - 192.0.2.1
      - 192.0.2.2
    proxied: true
  - name: api
    zone_name: example.org
    record_type: AAAA
    health_check:
      type: http
      endpoint: /status
      host: api.example.org
      timeout: 10
    failover_ips:
      - 2001:db8::1
      - 2001:db8::2
    return_to_priority: true
`

	config := loadConfigFromContent(t, "multizone_config_test_*.yaml", testConfigContent)

	if len(config.CloudflareZoneIDs) != 2 {
		t.Fatalf("Expected 2 zones, got %d", len(config.CloudflareZoneIDs))
	}
	if config.CloudflareZoneIDs[0].ZoneID != "zone-1" {
		t.Errorf("Expected first zone ID = 'zone-1', got '%s'", config.CloudflareZoneIDs[0].ZoneID)
	}
	if config.CloudflareZoneIDs[1].Name != "example.org" {
		t.Errorf("Expected second zone name = 'example.org', got '%s'", config.CloudflareZoneIDs[1].Name)
	}

	if len(config.Origins) != 2 {
		t.Fatalf("Expected 2 origins, got %d", len(config.Origins))
	}

	if len(config.Origins[0].PriorityFailoverIPs) != 2 {
		t.Errorf("Expected first origin PriorityFailoverIPs length = 2, got %d", len(config.Origins[0].PriorityFailoverIPs))
	}
	if !config.Origins[0].Proxied {
		t.Errorf("Expected first origin Proxied to be true")
	}

	if len(config.Origins[1].FailoverIPs) != 2 {
		t.Errorf("Expected second origin FailoverIPs length = 2, got %d", len(config.Origins[1].FailoverIPs))
	}
	if !config.Origins[1].ReturnToPriority {
		t.Errorf("Expected second origin ReturnToPriority to be true")
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

	path := createTempConfigFile(t, "invalid_config_*.json", invalidJSON)

	// 不正なJSONファイルを読み込む
	_, err := LoadConfig(path)
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

	config := loadConfigFromContent(t, "invalid_zone_config_test_*.json", invalidZoneConfigContent)

	if len(config.CloudflareZoneIDs) != 1 {
		t.Errorf("Expected 1 zone ID, got %d", len(config.CloudflareZoneIDs))
	}
	if len(config.Origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(config.Origins))
	}
}
