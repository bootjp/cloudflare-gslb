package mock

import (
	"context"
	"fmt"

	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	cf "github.com/cloudflare/cloudflare-go"
)

// DNSClientMock はCloudflare DNSクライアントのモック
type DNSClientMock struct {
	Records             map[string][]cf.DNSRecord
	GetDNSRecordsFunc   func(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error)
	DeleteDNSRecordFunc func(ctx context.Context, recordID string) error
	CreateDNSRecordFunc func(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error)
	UpdateDNSRecordFunc func(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error)
	ReplaceRecordsFunc  func(ctx context.Context, name, recordType, newContent string) error
}

// インターフェースに準拠していることを確認
var _ cloudflare.DNSClientInterface = (*DNSClientMock)(nil)

// NewDNSClientMock は新しいDNSClientMockを作成する
func NewDNSClientMock() *DNSClientMock {
	return &DNSClientMock{
		Records: make(map[string][]cf.DNSRecord),
	}
}

// GetDNSRecords はGetDNSRecordsFuncを呼び出すか、デフォルトの実装を使用する
func (m *DNSClientMock) GetDNSRecords(ctx context.Context, name, recordType string) ([]cf.DNSRecord, error) {
	if m.GetDNSRecordsFunc != nil {
		return m.GetDNSRecordsFunc(ctx, name, recordType)
	}

	key := fmt.Sprintf("%s-%s", name, recordType)
	records, ok := m.Records[key]
	if !ok {
		return []cf.DNSRecord{}, nil
	}
	return records, nil
}

// DeleteDNSRecord はDeleteDNSRecordFuncを呼び出すか、デフォルトの実装を使用する
func (m *DNSClientMock) DeleteDNSRecord(ctx context.Context, recordID string) error {
	if m.DeleteDNSRecordFunc != nil {
		return m.DeleteDNSRecordFunc(ctx, recordID)
	}

	// モックなのでレコードの削除は実際には行わない
	return nil
}

// CreateDNSRecord はCreateDNSRecordFuncを呼び出すか、デフォルトの実装を使用する
func (m *DNSClientMock) CreateDNSRecord(ctx context.Context, name, recordType, content string) (cf.DNSRecord, error) {
	if m.CreateDNSRecordFunc != nil {
		return m.CreateDNSRecordFunc(ctx, name, recordType, content)
	}

	// 新しいレコードを作成
	record := cf.DNSRecord{
		ID:      fmt.Sprintf("mock-record-%s-%s", name, recordType),
		Name:    name,
		Type:    recordType,
		Content: content,
	}

	// レコードをマップに追加
	key := fmt.Sprintf("%s-%s", name, recordType)
	m.Records[key] = append(m.Records[key], record)

	return record, nil
}

// UpdateDNSRecord はUpdateDNSRecordFuncを呼び出すか、デフォルトの実装を使用する
func (m *DNSClientMock) UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (cf.DNSRecord, error) {
	if m.UpdateDNSRecordFunc != nil {
		return m.UpdateDNSRecordFunc(ctx, recordID, name, recordType, content)
	}

	// 更新したレコードを返す
	return cf.DNSRecord{
		ID:      recordID,
		Name:    name,
		Type:    recordType,
		Content: content,
	}, nil
}

// ReplaceRecords はReplaceRecordsFuncを呼び出すか、デフォルトの実装を使用する
func (m *DNSClientMock) ReplaceRecords(ctx context.Context, name, recordType, newContent string) error {
	if m.ReplaceRecordsFunc != nil {
		return m.ReplaceRecordsFunc(ctx, name, recordType, newContent)
	}

	// レコードを置き換える
	key := fmt.Sprintf("%s-%s", name, recordType)
	m.Records[key] = []cf.DNSRecord{
		{
			ID:      fmt.Sprintf("mock-record-%s-%s", name, recordType),
			Name:    name,
			Type:    recordType,
			Content: newContent,
		},
	}

	return nil
}

func (m *DNSClientMock) GetZoneID() string {
	return "mock-zone-id"
}
