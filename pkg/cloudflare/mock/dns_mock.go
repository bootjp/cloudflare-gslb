package mock

import (
	"context"
	"fmt"

	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	"github.com/cloudflare/cloudflare-go/v6/dns"
)

type DNSClientMock struct {
	Records             map[string][]dns.RecordResponse
	GetDNSRecordsFunc   func(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error)
	DeleteDNSRecordFunc func(ctx context.Context, recordID string) error
	CreateDNSRecordFunc func(ctx context.Context, name, recordType, content string) (dns.RecordResponse, error)
	UpdateDNSRecordFunc func(ctx context.Context, recordID, name, recordType, content string) (dns.RecordResponse, error)
	ReplaceRecordsFunc  func(ctx context.Context, name, recordType string, newContents []string) error
}

var _ cloudflare.DNSClientInterface = (*DNSClientMock)(nil)

func NewDNSClientMock() *DNSClientMock {
	return &DNSClientMock{
		Records: make(map[string][]dns.RecordResponse),
	}
}

func (m *DNSClientMock) GetDNSRecords(ctx context.Context, name, recordType string) ([]dns.RecordResponse, error) {
	if m.GetDNSRecordsFunc != nil {
		return m.GetDNSRecordsFunc(ctx, name, recordType)
	}

	key := fmt.Sprintf("%s-%s", name, recordType)
	records, ok := m.Records[key]
	if !ok {
		return []dns.RecordResponse{}, nil
	}
	return records, nil
}

func (m *DNSClientMock) DeleteDNSRecord(ctx context.Context, recordID string) error {
	if m.DeleteDNSRecordFunc != nil {
		return m.DeleteDNSRecordFunc(ctx, recordID)
	}

	return nil
}

func (m *DNSClientMock) CreateDNSRecord(ctx context.Context, name, recordType, content string) (dns.RecordResponse, error) {
	if m.CreateDNSRecordFunc != nil {
		return m.CreateDNSRecordFunc(ctx, name, recordType, content)
	}

	record := dns.RecordResponse{
		ID:      fmt.Sprintf("mock-record-%s-%s", name, recordType),
		Name:    name,
		Type:    dns.RecordResponseType(recordType),
		Content: content,
	}

	key := fmt.Sprintf("%s-%s", name, recordType)
	m.Records[key] = append(m.Records[key], record)

	return record, nil
}

func (m *DNSClientMock) UpdateDNSRecord(ctx context.Context, recordID, name, recordType, content string) (dns.RecordResponse, error) {
	if m.UpdateDNSRecordFunc != nil {
		return m.UpdateDNSRecordFunc(ctx, recordID, name, recordType, content)
	}

	return dns.RecordResponse{
		ID:      recordID,
		Name:    name,
		Type:    dns.RecordResponseType(recordType),
		Content: content,
	}, nil
}

func (m *DNSClientMock) ReplaceRecords(ctx context.Context, name, recordType string, newContents []string) error {
	if m.ReplaceRecordsFunc != nil {
		return m.ReplaceRecordsFunc(ctx, name, recordType, newContents)
	}

	key := fmt.Sprintf("%s-%s", name, recordType)
	records := make([]dns.RecordResponse, 0, len(newContents))
	for i, content := range newContents {
		records = append(records, dns.RecordResponse{
			ID:      fmt.Sprintf("mock-record-%s-%s-%d", name, recordType, i),
			Name:    name,
			Type:    dns.RecordResponseType(recordType),
			Content: content,
		})
	}
	m.Records[key] = records

	return nil
}

func (m *DNSClientMock) GetZoneID() string {
	return "mock-zone-id"
}
