package gslb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
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
		config:    cfg,
		dnsClient: mockClient,
		stopCh:    make(chan struct{}),
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
