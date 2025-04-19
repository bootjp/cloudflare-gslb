package gslb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	cfmock "github.com/bootjp/cloudflare-gslb/pkg/cloudflare/mock"
	hcmock "github.com/bootjp/cloudflare-gslb/pkg/healthcheck/mock"
	cf "github.com/cloudflare/cloudflare-go"
)

// MockDNSClient はDNSClientインターフェースの独自実装
type MockDNSClient struct {
	*cfmock.DNSClientMock
}

// テスト用のサービスを作成するためのヘルパー関数
func createTestService() (*Service, *cfmock.DNSClientMock) {
	// テスト用の設定
	cfg := &config.Config{
		CloudflareAPIToken: "test-token",
		CloudflareZoneIDs: []config.ZoneConfig{
			{
				ZoneID: "test-zone",
				Name:   "default",
			},
		},
		CheckInterval: 1 * time.Second,
		Origins: []config.OriginConfig{
			{
				Name:       "example.com",
				ZoneName:   "default",
				RecordType: "A",
				HealthCheck: config.HealthCheck{
					Type:     "http",
					Endpoint: "/health",
					Timeout:  5,
				},
				FailoverIPs: []string{"192.168.1.2", "192.168.1.3"},
			},
		},
	}

	// DNSクライアントのモック
	dnsClientMock := cfmock.NewDNSClientMock()
	mockClient := &MockDNSClient{dnsClientMock}

	// サービスの作成
	service := &Service{
		config:          cfg,
		dnsClient:       mockClient,
		stopCh:          make(chan struct{}),
		failoverIndices: make(map[string]int),
		dnsClients:      make(map[string]cloudflare.DNSClientInterface),
		originStatus:    make(map[string]*OriginStatus),
		zoneMap:         map[string]string{"test-zone": "default"},
		zoneIDMap:       map[string]string{"default": "test-zone"},
	}

	// originStatusマップを初期化
	originKey := "default-example.com-A"
	service.originStatus[originKey] = &OriginStatus{
		CurrentIP:       "192.168.1.1",
		UsingPriority:   false,
		HealthyPriority: true,
		LastCheck:       time.Now(),
	}

	return service, dnsClientMock
}

// 拡張されたテスト用のサービスを作成するためのヘルパー関数
func createTestServiceWithPriorityConfig() (*Service, *cfmock.DNSClientMock) {
	// テスト用の設定（優先IPとフェイルオーバーIP設定を含む）
	cfg := &config.Config{
		CloudflareAPIToken: "test-token",
		CloudflareZoneIDs: []config.ZoneConfig{
			{
				ZoneID: "test-zone",
				Name:   "default",
			},
		},
		CheckInterval: 1 * time.Second,
		Origins: []config.OriginConfig{
			{
				Name:       "example.com",
				ZoneName:   "default",
				RecordType: "A",
				HealthCheck: config.HealthCheck{
					Type:     "http",
					Endpoint: "/health",
					Timeout:  5,
				},
				PriorityFailoverIPs: []string{"192.168.1.1"},
				FailoverIPs:         []string{"192.168.1.2", "192.168.1.3"},
				ReturnToPriority:    true,
			},
		},
	}

	// DNSクライアントのモック
	dnsClientMock := cfmock.NewDNSClientMock()
	mockClient := &MockDNSClient{dnsClientMock}

	// サービスの作成
	service := &Service{
		config:          cfg,
		dnsClient:       mockClient,
		stopCh:          make(chan struct{}),
		failoverIndices: make(map[string]int),
		dnsClients:      make(map[string]cloudflare.DNSClientInterface),
		originStatus:    make(map[string]*OriginStatus),
		zoneMap:         map[string]string{"test-zone": "default"},
		zoneIDMap:       map[string]string{"default": "test-zone"},
	}

	return service, dnsClientMock
}

func TestService_checkOrigin(t *testing.T) {
	tests := []struct {
		name              string
		records           []cf.DNSRecord
		checkError        error
		expectReplaceCall bool
	}{
		{
			name: "healthy record",
			records: []cf.DNSRecord{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    "A",
					Content: "192.168.1.1",
				},
			},
			checkError:        nil,
			expectReplaceCall: false,
		},
		{
			name: "unhealthy record",
			records: []cf.DNSRecord{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    "A",
					Content: "192.168.1.1",
				},
			},
			checkError:        errors.New("health check failed"),
			expectReplaceCall: true,
		},
		{
			name:              "no records",
			records:           []cf.DNSRecord{},
			checkError:        nil,
			expectReplaceCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestService()

			// レコードを設定
			key := "example.com-A"
			dnsClientMock.Records[key] = tt.records

			// GetDNSRecordsの振る舞いを設定
			dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error) {
				if name == "example.com" && recordType == "A" {
					return tt.records, nil
				}
				return []cf.DNSRecord{}, nil
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				return nil
			}

			// ヘルスチェッカーのモック
			checkerMock := hcmock.NewCheckerMock(func(ip string) error {
				return tt.checkError
			})

			// テスト対象のメソッドを実行
			service.checkOrigin(context.Background(), service.config.Origins[0], checkerMock)

			// 期待通りにReplaceRecordsが呼ばれたかチェック
			expectedCalls := 0
			if tt.expectReplaceCall {
				expectedCalls = 1
			}
			if replaceCallCount != expectedCalls {
				t.Errorf("ReplaceRecords was called %d times, expected %d", replaceCallCount, expectedCalls)
			}
		})
	}
}

func TestService_replaceUnhealthyRecord(t *testing.T) {
	tests := []struct {
		name          string
		recordType    string
		recordContent string
		failoverIPs   []string
		expectError   bool
	}{
		{
			name:          "replace A record",
			recordType:    "A",
			recordContent: "192.168.1.1",
			failoverIPs:   []string{"192.168.1.2", "192.168.1.3"},
			expectError:   false,
		},
		{
			name:          "replace AAAA record",
			recordType:    "AAAA",
			recordContent: "2001:db8::1",
			failoverIPs:   []string{"2001:db8::2", "2001:db8::3"},
			expectError:   false,
		},
		{
			name:          "invalid A record",
			recordType:    "A",
			recordContent: "invalid-ip",
			failoverIPs:   []string{"192.168.1.2", "192.168.1.3"},
			expectError:   true,
		},
		{
			name:          "unsupported record type",
			recordType:    "CNAME",
			recordContent: "example.com",
			failoverIPs:   []string{},
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestService()

			// オリジン設定を更新（テストケースに合わせて）
			origin := config.OriginConfig{
				Name:        "example.com",
				ZoneName:    "default",
				RecordType:  tt.recordType,
				FailoverIPs: tt.failoverIPs,
			}

			// originStatusマップを初期化（各テストケース用）
			originKey := fmt.Sprintf("default-example.com-%s", tt.recordType)
			service.originStatus[originKey] = &OriginStatus{
				CurrentIP:       tt.recordContent,
				UsingPriority:   false,
				HealthyPriority: true,
				LastCheck:       time.Now(),
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				return nil
			}

			// 不健全なレコードを作成
			unhealthyRecord := cf.DNSRecord{
				ID:      "record-1",
				Name:    "example.com",
				Type:    tt.recordType,
				Content: tt.recordContent,
			}

			// テスト対象のメソッドを実行
			err := service.replaceUnhealthyRecord(context.Background(), origin, unhealthyRecord)

			// 期待通りのエラー発生をチェック
			if (err != nil) != tt.expectError {
				t.Errorf("replaceUnhealthyRecord() error = %v, expectError %v", err, tt.expectError)
				return
			}

			// エラーが期待されている場合は以降のチェックをスキップ
			if tt.expectError {
				return
			}

			// ReplaceRecordsが呼ばれたかチェック
			if len(tt.failoverIPs) > 0 && replaceCallCount != 1 {
				t.Errorf("ReplaceRecords was called %d times, expected 1", replaceCallCount)
			}
		})
	}
}

// TestIPandStatusSync 今回の問題を検出するテスト：IPと状態の同期に関するテスト
func TestIPandStatusSync(t *testing.T) {
	tests := []struct {
		name                  string
		currentIP             string
		usingPriority         bool
		expectedUsingPriority bool
		expectedReplaceCall   bool
	}{
		{
			name:                  "state inconsistency: using priority=true but IP is failover",
			currentIP:             "192.168.1.2", // フェイルオーバーIP
			usingPriority:         true,          // 優先IPを使用中と状態は示しているが...
			expectedUsingPriority: false,         // 実際のIPを検査すると優先IPではないので修正される
			expectedReplaceCall:   false,         // IPの修正は必要ない（状態の修正のみ）
		},
		{
			name:                  "state inconsistency: using priority=false but IP is priority",
			currentIP:             "192.168.1.1", // 優先IP
			usingPriority:         false,         // 優先IPを使用していないと状態は示しているが...
			expectedUsingPriority: true,          // 実際のIPを検査すると優先IPなので修正される
			expectedReplaceCall:   false,         // IPの修正は必要ない（状態の修正のみ）
		},
		{
			name:                  "correct state: using priority=true and IP is priority",
			currentIP:             "192.168.1.1", // 優先IP
			usingPriority:         true,          // 優先IPを使用中と状態も正しい
			expectedUsingPriority: true,          // 状態は維持される
			expectedReplaceCall:   false,         // 修正は不要
		},
		{
			name:                  "correct state: using priority=false and IP is failover",
			currentIP:             "192.168.1.2", // フェイルオーバーIP
			usingPriority:         false,         // 優先IPを使用していないと状態も正しい
			expectedUsingPriority: false,         // 状態は維持される
			expectedReplaceCall:   false,         // 修正は不要
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestServiceWithPriorityConfig()

			origin := service.config.Origins[0]
			ctx := context.Background()

			// originStatusを初期化
			originKey := "default-example.com-A"
			service.originStatus[originKey] = &OriginStatus{
				CurrentIP:       tt.currentIP,
				UsingPriority:   tt.usingPriority,
				HealthyPriority: true,
				LastCheck:       time.Now(),
			}

			// モックのレコードを設定
			dnsClientMock.Records["example.com-A"] = []cf.DNSRecord{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    "A",
					Content: tt.currentIP,
				},
			}

			// GetDNSRecordsの振る舞いを設定
			dnsClientMock.GetDNSRecordsFunc = func(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error) {
				key := fmt.Sprintf("%s-%s", name, recordType)
				return dnsClientMock.Records[key], nil
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				return nil
			}

			// ヘルスチェッカーのモック
			checker := hcmock.NewCheckerMock(func(ip string) error {
				// すべてのIPが正常と見なす
				return nil
			})

			// テスト対象のメソッドを実行
			service.processRecord(ctx, origin, dnsClientMock.Records["example.com-A"][0], checker, service.originStatus[originKey])

			// 期待通りに状態が更新されたか確認
			if service.originStatus[originKey].UsingPriority != tt.expectedUsingPriority {
				t.Errorf("UsingPriority = %v, expected %v", service.originStatus[originKey].UsingPriority, tt.expectedUsingPriority)
			}

			// 期待通りにReplaceRecordsが呼ばれたか確認
			if (replaceCallCount > 0) != tt.expectedReplaceCall {
				t.Errorf("ReplaceRecords was called %d times, expected %v", replaceCallCount, tt.expectedReplaceCall)
			}
		})
	}
}

// TestReturnToPriorityTrigger 優先IPに戻る条件をテスト
func TestReturnToPriorityTrigger(t *testing.T) {
	tests := []struct {
		name                string
		returnToPriority    bool
		currentIP           string
		usingPriority       bool
		healthyPriority     bool
		expectPriorityCheck bool
		expectReplaceCall   bool
	}{
		{
			name:                "should return to priority: enabled and priority healthy",
			returnToPriority:    true,
			currentIP:           "192.168.1.2", // フェイルオーバーIP
			usingPriority:       false,         // 優先IPを使用していない
			healthyPriority:     true,          // 優先IPは健全
			expectPriorityCheck: true,
			expectReplaceCall:   true,
		},
		{
			name:                "should not return to priority: disabled",
			returnToPriority:    false,
			currentIP:           "192.168.1.2", // フェイルオーバーIP
			usingPriority:       false,         // 優先IPを使用していない
			healthyPriority:     true,          // 優先IPは健全
			expectPriorityCheck: false,
			expectReplaceCall:   false,
		},
		{
			name:                "should not return to priority: already using priority",
			returnToPriority:    true,
			currentIP:           "192.168.1.1", // 優先IP
			usingPriority:       true,          // 優先IPを使用中
			healthyPriority:     true,          // 優先IPは健全
			expectPriorityCheck: false,
			expectReplaceCall:   false,
		},
		{
			name:                "should not return to priority: priority unhealthy",
			returnToPriority:    true,
			currentIP:           "192.168.1.2", // フェイルオーバーIP
			usingPriority:       false,         // 優先IPを使用していない
			healthyPriority:     false,         // 優先IPは不健全
			expectPriorityCheck: true,
			expectReplaceCall:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestServiceWithPriorityConfig()

			// オリジン設定をテストケースに合わせて更新
			origin := service.config.Origins[0]
			origin.ReturnToPriority = tt.returnToPriority

			// ヘルスチェッカーのモック - 優先IPのヘルスチェック結果をテストケースに応じて調整
			checker := hcmock.NewCheckerMock(func(ip string) error {
				if ip == origin.PriorityFailoverIPs[0] && !tt.healthyPriority {
					return fmt.Errorf("priority IP is unhealthy")
				}
				return nil // その他のIPは正常と見なす
			})

			// dnsClientsマップにモッククライアントを追加
			originKey := "default-example.com-A"
			service.dnsClients[originKey] = dnsClientMock

			// originStatusを初期化
			service.originStatus[originKey] = &OriginStatus{
				CurrentIP:       tt.currentIP,
				UsingPriority:   tt.usingPriority,
				HealthyPriority: tt.healthyPriority,
				LastCheck:       time.Now(),
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				service.originStatus[originKey].CurrentIP = newContent
				service.originStatus[originKey].UsingPriority = true
				return nil
			}

			// テスト対象のメソッドを実行
			ctx := context.Background()
			if origin.ReturnToPriority && len(origin.PriorityFailoverIPs) > 0 {
				service.checkPriorityIPs(ctx, origin, checker)
			}

			// 期待通りにReplaceRecordsが呼ばれたか確認
			if tt.expectReplaceCall && replaceCallCount == 0 {
				t.Errorf("ReplaceRecords was called %d times, expected at least 1", replaceCallCount)
			} else if !tt.expectReplaceCall && replaceCallCount > 0 {
				t.Errorf("ReplaceRecords was called %d times, expected 0", replaceCallCount)
			}
		})
	}
}
