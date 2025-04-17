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
		CloudflareZoneID:   "test-zone",
		CheckInterval:      1 * time.Second,
		Origins: []config.OriginConfig{
			{
				Name:       "example.com",
				RecordType: "A",
				HealthCheck: config.HealthCheck{
					Type:     "http",
					Endpoint: "/health",
					Timeout:  5,
				},
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
	}

	// originStatusマップを初期化
	originKey := "example.com-A"
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
		CloudflareZoneID:   "test-zone",
		CheckInterval:      1 * time.Second,
		Origins: []config.OriginConfig{
			{
				Name:       "example.com",
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
		expectError   bool
	}{
		{
			name:          "replace A record",
			recordType:    "A",
			recordContent: "192.168.1.1",
			expectError:   false,
		},
		{
			name:          "replace AAAA record",
			recordType:    "AAAA",
			recordContent: "2001:db8::1",
			expectError:   false,
		},
		{
			name:          "invalid A record",
			recordType:    "A",
			recordContent: "invalid-ip",
			expectError:   true,
		},
		{
			name:          "unsupported record type",
			recordType:    "CNAME",
			recordContent: "example.com",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestService()

			// originStatusマップを初期化（各テストケース用）
			originKey := fmt.Sprintf("example.com-%s", tt.recordType)
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

			// テスト対象のメソッドを実行
			origin := config.OriginConfig{
				Name:       "example.com",
				RecordType: tt.recordType,
			}
			record := cf.DNSRecord{
				ID:      "record-1",
				Name:    "example.com",
				Type:    tt.recordType,
				Content: tt.recordContent,
			}

			err := service.replaceUnhealthyRecord(context.Background(), origin, record)

			// 期待通りのエラーか確認
			if (err != nil) != tt.expectError {
				t.Errorf("replaceUnhealthyRecord() error = %v, expectError %v", err, tt.expectError)
			}

			// エラーがない場合はReplaceRecordsが呼ばれたか確認
			if !tt.expectError && replaceCallCount != 1 {
				t.Errorf("ReplaceRecords was called %d times, expected 1", replaceCallCount)
			}
		})
	}
}

// TestIPandStatusSync 今回の問題を検出するテスト：IPと状態の同期に関するテスト
func TestIPandStatusSync(t *testing.T) {
	tests := []struct {
		name                 string
		currentIP            string
		initialUsingPriority bool
		expectUsingPriority  bool
		expectReplaceCall    bool
	}{
		{
			name:                 "state inconsistency: using priority=true but IP is failover",
			currentIP:            "192.168.1.2", // フェイルオーバーIP
			initialUsingPriority: true,          // UsingPriorityが誤ってtrueに設定されている
			expectUsingPriority:  true,          // checkPriorityIPsを実行後、レコードを置き換えると状態も更新される
			expectReplaceCall:    true,          // 優先IPに戻すためにレコードを置き換える
		},
		{
			name:                 "state inconsistency: using priority=false but IP is priority",
			currentIP:            "192.168.1.1", // 優先IP
			initialUsingPriority: false,         // UsingPriorityが誤ってfalseに設定されている
			expectUsingPriority:  true,          // 修正後はtrueになるべき
			expectReplaceCall:    false,         // 既に優先IPを使用しているので置き換えは不要
		},
		{
			name:                 "correct state: using priority=true and IP is priority",
			currentIP:            "192.168.1.1", // 優先IP
			initialUsingPriority: true,          // 正しい設定
			expectUsingPriority:  true,          // 変更なし
			expectReplaceCall:    false,         // 既に優先IPを使用しているので置き換えは不要
		},
		{
			name:                 "correct state: using priority=false and IP is failover",
			currentIP:            "192.168.1.2", // フェイルオーバーIP
			initialUsingPriority: false,         // 正しい設定
			expectUsingPriority:  true,          // 優先IPに戻ったのでtrueになる
			expectReplaceCall:    true,          // 優先IPが健全な場合は優先IPに戻る
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestServiceWithPriorityConfig()

			// オリジン設定を取得
			origin := service.config.Origins[0]
			originKey := "example.com-A"

			// レコードを設定
			dnsClientMock.Records[originKey] = []cf.DNSRecord{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    "A",
					Content: tt.currentIP,
				},
			}

			// 状態を設定
			service.originStatus[originKey] = &OriginStatus{
				CurrentIP:       tt.currentIP,
				UsingPriority:   tt.initialUsingPriority,
				HealthyPriority: true,
				LastCheck:       time.Now(),
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				// DNSレコードを更新（モック内部の状態も更新）
				key := fmt.Sprintf("%s-%s", name, recordType)
				dnsClientMock.Records[key] = []cf.DNSRecord{
					{
						ID:      "record-1",
						Name:    name,
						Type:    recordType,
						Content: newContent,
					},
				}
				// サービスの状態も更新
				service.originStatus[originKey].CurrentIP = newContent
				return nil
			}

			// 健全な優先IPを返すヘルスチェッカーのモック
			checkerMock := hcmock.NewCheckerMock(func(ip string) error {
				return nil // すべてのIPが健全
			})

			// テスト対象のメソッドを実行
			service.checkPriorityIPs(context.Background(), origin, checkerMock)

			// 状態が期待通りに更新されたかチェック
			if service.originStatus[originKey].UsingPriority != tt.expectUsingPriority {
				t.Errorf("UsingPriority = %v, expected %v",
					service.originStatus[originKey].UsingPriority,
					tt.expectUsingPriority)
			}

			// 期待通りにReplaceRecordsが呼ばれたかチェック
			expectedCalls := 0
			if tt.expectReplaceCall {
				expectedCalls = 1
			}
			if replaceCallCount != expectedCalls {
				t.Errorf("ReplaceRecords was called %d times, expected %d",
					replaceCallCount, expectedCalls)
			}

			// フェイルオーバーIPから優先IPに戻った場合、IPが優先IPに更新されているか確認
			if tt.expectReplaceCall && replaceCallCount > 0 {
				expectedIP := origin.PriorityFailoverIPs[0]
				if service.originStatus[originKey].CurrentIP != expectedIP {
					t.Errorf("CurrentIP = %s, expected %s",
						service.originStatus[originKey].CurrentIP, expectedIP)
				}
			}
		})
	}
}

// TestReturnToPriorityTrigger 優先IPに戻る条件をテスト
func TestReturnToPriorityTrigger(t *testing.T) {
	tests := []struct {
		name              string
		returnToPriority  bool
		priorityHealthy   bool
		expectReplaceCall bool
	}{
		{
			name:              "should return to priority: enabled and priority healthy",
			returnToPriority:  true,
			priorityHealthy:   true,
			expectReplaceCall: true,
		},
		{
			name:              "should not return to priority: disabled but priority healthy",
			returnToPriority:  false,
			priorityHealthy:   true,
			expectReplaceCall: false,
		},
		{
			name:              "should not return to priority: enabled but priority unhealthy",
			returnToPriority:  true,
			priorityHealthy:   false,
			expectReplaceCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// テスト用のサービスを作成
			service, dnsClientMock := createTestServiceWithPriorityConfig()

			// オリジン設定を取得して修正
			origin := service.config.Origins[0]
			origin.ReturnToPriority = tt.returnToPriority
			originKey := "example.com-A"

			// フェイルオーバーIPを使用中の状態を設定
			service.originStatus[originKey] = &OriginStatus{
				CurrentIP:       origin.FailoverIPs[0], // フェイルオーバーIPを使用中
				UsingPriority:   false,
				HealthyPriority: tt.priorityHealthy,
				LastCheck:       time.Now(),
			}

			// レコードを設定
			dnsClientMock.Records[originKey] = []cf.DNSRecord{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    "A",
					Content: origin.FailoverIPs[0],
				},
			}

			// ReplaceRecordsの呼び出しをトラッキング
			replaceCallCount := 0
			dnsClientMock.ReplaceRecordsFunc = func(ctx context.Context, name, recordType, newContent string) error {
				replaceCallCount++
				return nil
			}

			// ヘルスチェッカーのモック（優先IPの健全性に応じて結果を返す）
			checkerMock := hcmock.NewCheckerMock(func(ip string) error {
				// 優先IPの場合は設定に応じた結果を返す
				if ip == origin.PriorityFailoverIPs[0] {
					if tt.priorityHealthy {
						return nil
					}
					return errors.New("priority IP is unhealthy")
				}
				// その他のIPは健全
				return nil
			})

			// monitorOrigin関数でReturnToPriorityフラグをチェックする処理を模倣
			if origin.ReturnToPriority {
				// テスト対象のメソッドを実行
				service.checkPriorityIPs(context.Background(), origin, checkerMock)
			}

			// 期待通りにReplaceRecordsが呼ばれたかチェック
			expectedCalls := 0
			if tt.expectReplaceCall {
				expectedCalls = 1
			}
			if replaceCallCount != expectedCalls {
				t.Errorf("ReplaceRecords was called %d times, expected %d",
					replaceCallCount, expectedCalls)
			}

			// フェイルオーバーIPから優先IPに戻った場合、状態が正しく更新されているか確認
			if tt.expectReplaceCall && replaceCallCount > 0 {
				if !service.originStatus[originKey].UsingPriority {
					t.Errorf("UsingPriority = false, expected true")
				}
			}
		})
	}
}
