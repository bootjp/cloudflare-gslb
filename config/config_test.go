package config

import (
	"os"
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

// TestPriorityFailoverIPsBackwardCompatibility tests backward compatibility with string array format
func TestPriorityFailoverIPsBackwardCompatibility(t *testing.T) {
	// Old format: priority_failover_ips as a string array
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "www",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/health",
					"host": "www.example.com",
					"timeout": 5
				},
				"priority_failover_ips": ["192.168.1.1", "192.168.1.2"],
				"failover_ips": ["192.168.1.3", "192.168.1.4"]
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

	// Check that the priority IPs were correctly parsed with default priorities
	origin := config.Origins[0]
	if len(origin.PriorityFailoverIPs) != 2 {
		t.Errorf("Expected 2 priority failover IPs, got %d", len(origin.PriorityFailoverIPs))
	}
	if origin.PriorityFailoverIPs[0].IP != "192.168.1.1" {
		t.Errorf("Expected first priority IP = '192.168.1.1', got '%s'", origin.PriorityFailoverIPs[0].IP)
	}
	if origin.PriorityFailoverIPs[0].Priority != 0 {
		t.Errorf("Expected first priority value = 0, got %d", origin.PriorityFailoverIPs[0].Priority)
	}
	if origin.PriorityFailoverIPs[1].IP != "192.168.1.2" {
		t.Errorf("Expected second priority IP = '192.168.1.2', got '%s'", origin.PriorityFailoverIPs[1].IP)
	}
	if origin.PriorityFailoverIPs[1].Priority != 1 {
		t.Errorf("Expected second priority value = 1, got %d", origin.PriorityFailoverIPs[1].Priority)
	}
}

// TestPriorityFailoverIPsNewFormat tests the new format with explicit priority values
func TestPriorityFailoverIPsNewFormat(t *testing.T) {
	// New format: priority_failover_ips as an array of objects with priority
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "www",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/health",
					"host": "www.example.com",
					"timeout": 5
				},
				"priority_failover_ips": [
					{"ip": "192.168.1.3", "priority": 2},
					{"ip": "192.168.1.1", "priority": 0},
					{"ip": "192.168.1.2", "priority": 1}
				],
				"failover_ips": ["192.168.1.4", "192.168.1.5"]
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

	// Check that the priority IPs were correctly parsed with explicit priorities
	origin := config.Origins[0]
	if len(origin.PriorityFailoverIPs) != 3 {
		t.Errorf("Expected 3 priority failover IPs, got %d", len(origin.PriorityFailoverIPs))
	}

	// IPs should be stored in the order they were defined
	if origin.PriorityFailoverIPs[0].IP != "192.168.1.3" {
		t.Errorf("Expected first priority IP = '192.168.1.3', got '%s'", origin.PriorityFailoverIPs[0].IP)
	}
	if origin.PriorityFailoverIPs[0].Priority != 2 {
		t.Errorf("Expected first priority value = 2, got %d", origin.PriorityFailoverIPs[0].Priority)
	}

	// GetPriorityIPs should return IPs sorted by priority
	sortedIPs := origin.GetPriorityIPs()
	if len(sortedIPs) != 3 {
		t.Errorf("Expected 3 sorted priority IPs, got %d", len(sortedIPs))
	}
	if sortedIPs[0] != "192.168.1.1" {
		t.Errorf("Expected first sorted IP = '192.168.1.1', got '%s'", sortedIPs[0])
	}
	if sortedIPs[1] != "192.168.1.2" {
		t.Errorf("Expected second sorted IP = '192.168.1.2', got '%s'", sortedIPs[1])
	}
	if sortedIPs[2] != "192.168.1.3" {
		t.Errorf("Expected third sorted IP = '192.168.1.3', got '%s'", sortedIPs[2])
	}
}

// TestIsPriorityIP tests the IsPriorityIP method
func TestIsPriorityIP(t *testing.T) {
	origin := OriginConfig{
		PriorityFailoverIPs: []PriorityIP{
			{IP: "192.168.1.1", Priority: 0},
			{IP: "192.168.1.2", Priority: 1},
		},
		FailoverIPs: []string{"192.168.1.3"},
	}

	if !origin.IsPriorityIP("192.168.1.1") {
		t.Error("Expected 192.168.1.1 to be a priority IP")
	}
	if !origin.IsPriorityIP("192.168.1.2") {
		t.Error("Expected 192.168.1.2 to be a priority IP")
	}
	if origin.IsPriorityIP("192.168.1.3") {
		t.Error("Expected 192.168.1.3 to NOT be a priority IP")
	}
	if origin.IsPriorityIP("192.168.1.4") {
		t.Error("Expected 192.168.1.4 to NOT be a priority IP")
	}
}

// TestGetPriorityIPs tests the GetPriorityIPs method
func TestGetPriorityIPs(t *testing.T) {
	// Test with empty list
	emptyOrigin := OriginConfig{}
	if emptyOrigin.GetPriorityIPs() != nil {
		t.Error("Expected nil for empty priority IPs")
	}

	// Test sorting by priority
	origin := OriginConfig{
		PriorityFailoverIPs: []PriorityIP{
			{IP: "192.168.1.3", Priority: 2},
			{IP: "192.168.1.1", Priority: 0},
			{IP: "192.168.1.2", Priority: 1},
		},
	}

	sortedIPs := origin.GetPriorityIPs()
	expected := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for i, ip := range sortedIPs {
		if ip != expected[i] {
			t.Errorf("Expected IP at index %d = '%s', got '%s'", i, expected[i], ip)
		}
	}

	// Test with empty IP strings (edge case)
	emptyIPsOrigin := OriginConfig{
		PriorityFailoverIPs: []PriorityIP{
			{IP: "", Priority: 0},
			{IP: "", Priority: 1},
		},
	}
	// GetPriorityIPs should still return the IPs even if they are empty
	// The system should handle this gracefully
	emptyIPs := emptyIPsOrigin.GetPriorityIPs()
	if len(emptyIPs) != 2 {
		t.Errorf("Expected 2 IPs, got %d", len(emptyIPs))
	}
}

// TestParsePriorityFailoverIPsEdgeCases tests edge cases in priority IP parsing
func TestParsePriorityFailoverIPsEdgeCases(t *testing.T) {
	// Test with mixed empty and non-empty IPs (new format)
	// This should fall back to string array parsing since not all IPs are valid
	testConfigContent := `{
		"cloudflare_api_token": "test-token",
		"cloudflare_zone_id": "test-zone",
		"check_interval_seconds": 60,
		"origins": [
			{
				"name": "www",
				"record_type": "A",
				"health_check": {
					"type": "http",
					"endpoint": "/health",
					"host": "www.example.com",
					"timeout": 5
				},
				"priority_failover_ips": [
					{"ip": "192.168.1.1", "priority": 0},
					{"ip": "", "priority": 1}
				],
				"failover_ips": ["192.168.1.3"]
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

	// This should fail to parse as new format (due to empty IP) and try string format
	// String format parsing will also fail since the structure is not a string array
	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("Expected error for config with empty IP in new format, but got none")
	}
}
