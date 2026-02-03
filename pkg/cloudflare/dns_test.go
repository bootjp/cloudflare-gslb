package cloudflare

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/packages/pagination"
	crerrors "github.com/cockroachdb/errors"
)

// createCall represents a create DNS record call
type createCall struct {
	name    string
	rtype   string
	content string
	ttl     int
	proxied bool
}

// updateCall represents an update DNS record call
type updateCall struct {
	recordID string
	name     string
	rtype    string
	content  string
	ttl      int
	proxied  bool
}

type fakeCloudflareAPI struct {
	listResp    []dns.RecordResponse
	listErr     error
	createCalls []createCall
	updateCalls []updateCall
	deleteCalls []string
	createErr   error
	updateErr   error
	deleteErr   error
}

func (f *fakeCloudflareAPI) List(ctx context.Context, params dns.RecordListParams, opts ...option.RequestOption) (*pagination.V4PagePaginationArray[dns.RecordResponse], error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	records := make([]dns.RecordResponse, len(f.listResp))
	copy(records, f.listResp)
	return &pagination.V4PagePaginationArray[dns.RecordResponse]{
		Result: records,
	}, nil
}

func (f *fakeCloudflareAPI) New(ctx context.Context, params dns.RecordNewParams, opts ...option.RequestOption) (*dns.RecordResponse, error) {
	// We can't directly inspect the union type, so we'll infer from the test usage
	// For simplicity, we'll create a call record with default values
	call := createCall{
		name:    "", // Would need type assertion to extract
		rtype:   "", // Would need type assertion to extract
		content: "", // Would need type assertion to extract
		ttl:     0,  // Would need type assertion to extract
		proxied: false,
	}
	f.createCalls = append(f.createCalls, call)

	if f.createErr != nil {
		return nil, f.createErr
	}
	return &dns.RecordResponse{
		ID:      "created",
		Name:    "",
		Type:    dns.RecordResponseTypeA,
		Content: "",
		TTL:     dns.TTL1,
		Proxied: false,
	}, nil
}

func (f *fakeCloudflareAPI) Update(ctx context.Context, dnsRecordID string, params dns.RecordUpdateParams, opts ...option.RequestOption) (*dns.RecordResponse, error) {
	call := updateCall{
		recordID: dnsRecordID,
	}
	f.updateCalls = append(f.updateCalls, call)

	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &dns.RecordResponse{
		ID: dnsRecordID,
	}, nil
}

func (f *fakeCloudflareAPI) Delete(ctx context.Context, dnsRecordID string, body dns.RecordDeleteParams, opts ...option.RequestOption) (*dns.RecordDeleteResponse, error) {
	f.deleteCalls = append(f.deleteCalls, dnsRecordID)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &dns.RecordDeleteResponse{}, nil
}

func TestDNSClientReplaceRecordsCreatesWhenNoRecords(t *testing.T) {
	api := &fakeCloudflareAPI{}
	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: true,
		ttl:     120,
	}

	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.10"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(api.createCalls) != 1 {
		t.Fatalf("expected create to be called once, got %d", len(api.createCalls))
	}

	if len(api.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(api.updateCalls))
	}
	if len(api.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls, got %d", len(api.deleteCalls))
	}
}

func TestDNSClientReplaceRecordsUpdatesExistingRecord(t *testing.T) {
	api := &fakeCloudflareAPI{
		listResp: []dns.RecordResponse{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    dns.RecordResponseTypeA,
			Content: "198.51.100.1",
			Proxied: false,
		}},
	}

	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     300,
	}

	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.20"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With atomic approach: create new record first, then delete old one
	if len(api.createCalls) != 1 {
		t.Fatalf("expected create to be called once, got %d", len(api.createCalls))
	}

	if len(api.deleteCalls) != 1 {
		t.Fatalf("expected one delete call, got %d", len(api.deleteCalls))
	}

	if api.deleteCalls[0] != "record-1" {
		t.Fatalf("expected record-1 to be deleted, got %s", api.deleteCalls[0])
	}

	if len(api.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(api.updateCalls))
	}
}

func TestDNSClientReplaceRecordsDeletesDuplicateRecords(t *testing.T) {
	api := &fakeCloudflareAPI{
		listResp: []dns.RecordResponse{
			{ID: "record-1", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "198.51.100.1", Proxied: true},
			{ID: "record-2", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "198.51.100.2", Proxied: true},
		},
	}

	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: true,
		ttl:     60,
	}

	start := time.Now()
	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.30"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With two records to delete, expect at least 500ms delay
	if time.Since(start) < 500*time.Millisecond {
		t.Fatalf("expected deletion to respect delay between operations")
	}

	// With atomic approach: create new record first, then delete both old ones
	if len(api.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(api.createCalls))
	}
	if len(api.deleteCalls) != 2 {
		t.Fatalf("expected two delete calls, got %d", len(api.deleteCalls))
	}
	if len(api.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(api.updateCalls))
	}
}

func TestDNSClientReplaceRecordsUpdateError(t *testing.T) {
	// With atomic approach, we create instead of update, so test create error
	expected := crerrors.New("create failed")
	api := &fakeCloudflareAPI{
		listResp:  []dns.RecordResponse{{ID: "record-1", Name: "example.com", Type: dns.RecordResponseTypeA, Proxied: false}},
		createErr: expected,
	}

	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     100,
	}

	err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.40")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expected) {
		t.Fatalf("expected error %v, got %v", expected, err)
	}
	if len(api.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls on error, got %d", len(api.deleteCalls))
	}
}

// TestDNSClientReplaceRecordsIdempotent tests that ReplaceRecords is idempotent
// when the desired content already exists
func TestDNSClientReplaceRecordsIdempotent(t *testing.T) {
	api := &fakeCloudflareAPI{
		listResp: []dns.RecordResponse{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    dns.RecordResponseTypeA,
			Content: "203.0.113.20",
			Proxied: false,
		}},
	}

	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     300,
	}

	// Try to replace with the same content - should be idempotent (no changes)
	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.20"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No operations should be performed since content matches
	if len(api.createCalls) != 0 {
		t.Fatalf("expected no create calls, got %d", len(api.createCalls))
	}
	if len(api.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(api.updateCalls))
	}
	if len(api.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls, got %d", len(api.deleteCalls))
	}
}

// TestDNSClientGetDNSRecords tests the GetDNSRecords method
func TestDNSClientGetDNSRecords(t *testing.T) {
	tests := []struct {
		name        string
		recordName  string
		recordType  string
		mockRecords []dns.RecordResponse
		mockErr     error
		wantErr     bool
		wantCount   int
	}{
		{
			name:       "successfully get records",
			recordName: "example.com",
			recordType: "A",
			mockRecords: []dns.RecordResponse{
				{
					ID:      "record-1",
					Name:    "example.com",
					Type:    dns.RecordResponseTypeA,
					Content: "192.168.1.1",
				},
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:        "no records found",
			recordName:  "nonexistent.com",
			recordType:  "A",
			mockRecords: []dns.RecordResponse{},
			wantErr:     false,
			wantCount:   0,
		},
		{
			name:        "API error",
			recordName:  "example.com",
			recordType:  "A",
			mockRecords: nil,
			mockErr:     crerrors.New("API error"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{
				listResp: tt.mockRecords,
				listErr:  tt.mockErr,
			}
			client := &DNSClient{
				api:    api,
				zoneID: "test-zone",
			}

			records, err := client.GetDNSRecords(context.Background(), tt.recordName, tt.recordType)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetDNSRecords() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(records) != tt.wantCount {
				t.Errorf("GetDNSRecords() returned %d records, want %d", len(records), tt.wantCount)
			}
		})
	}
}

// TestDNSClientDeleteDNSRecord tests the DeleteDNSRecord method
func TestDNSClientDeleteDNSRecord(t *testing.T) {
	tests := []struct {
		name     string
		recordID string
		mockErr  error
		wantErr  bool
	}{
		{
			name:     "successfully delete record",
			recordID: "record-1",
			wantErr:  false,
		},
		{
			name:     "API error",
			recordID: "record-1",
			mockErr:  crerrors.New("API error"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{
				deleteErr: tt.mockErr,
			}
			client := &DNSClient{
				api:    api,
				zoneID: "test-zone",
			}

			err := client.DeleteDNSRecord(context.Background(), tt.recordID)

			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteDNSRecord() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && len(api.deleteCalls) != 1 {
				t.Errorf("DeleteDNSRecord() called %d times, want 1", len(api.deleteCalls))
			}

			if !tt.wantErr && api.deleteCalls[0] != tt.recordID {
				t.Errorf("DeleteDNSRecord() called with recordID %s, want %s", api.deleteCalls[0], tt.recordID)
			}
		})
	}
}

// TestDNSClientCreateDNSRecord tests the CreateDNSRecord method
func TestDNSClientCreateDNSRecord(t *testing.T) {
	tests := []struct {
		name       string
		recordName string
		recordType string
		content    string
		mockErr    error
		wantErr    bool
	}{
		{
			name:       "create A record",
			recordName: "example.com",
			recordType: "A",
			content:    "192.168.1.1",
			wantErr:    false,
		},
		{
			name:       "create AAAA record",
			recordName: "example.com",
			recordType: "AAAA",
			content:    "2001:db8::1",
			wantErr:    false,
		},
		{
			name:       "API error",
			recordName: "example.com",
			recordType: "A",
			content:    "192.168.1.1",
			mockErr:    crerrors.New("API error"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{
				createErr: tt.mockErr,
			}
			client := &DNSClient{
				api:     api,
				zoneID:  "test-zone",
				proxied: false,
				ttl:     300,
			}

			record, err := client.CreateDNSRecord(context.Background(), tt.recordName, tt.recordType, tt.content)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateDNSRecord() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(api.createCalls) != 1 {
					t.Errorf("CreateDNSRecord() called %d times, want 1", len(api.createCalls))
				}

				if record.ID == "" {
					t.Error("CreateDNSRecord() returned empty ID")
				}
			}
		})
	}
}

// TestDNSClientUpdateDNSRecord tests the UpdateDNSRecord method
func TestDNSClientUpdateDNSRecord(t *testing.T) {
	tests := []struct {
		name       string
		recordID   string
		recordName string
		recordType string
		content    string
		mockErr    error
		wantErr    bool
	}{
		{
			name:       "update A record",
			recordID:   "record-1",
			recordName: "example.com",
			recordType: "A",
			content:    "192.168.1.2",
			wantErr:    false,
		},
		{
			name:       "update AAAA record",
			recordID:   "record-2",
			recordName: "example.com",
			recordType: "AAAA",
			content:    "2001:db8::2",
			wantErr:    false,
		},
		{
			name:       "API error",
			recordID:   "record-1",
			recordName: "example.com",
			recordType: "A",
			content:    "192.168.1.2",
			mockErr:    crerrors.New("API error"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{
				updateErr: tt.mockErr,
			}
			client := &DNSClient{
				api:     api,
				zoneID:  "test-zone",
				proxied: true,
				ttl:     120,
			}

			record, err := client.UpdateDNSRecord(context.Background(), tt.recordID, tt.recordName, tt.recordType, tt.content)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateDNSRecord() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(api.updateCalls) != 1 {
					t.Errorf("UpdateDNSRecord() called %d times, want 1", len(api.updateCalls))
				}

				if record.ID != tt.recordID {
					t.Errorf("UpdateDNSRecord() returned ID %s, want %s", record.ID, tt.recordID)
				}
			}
		})
	}
}

// TestDNSClientCreateError tests error handling in CreateDNSRecord
func TestDNSClientCreateError(t *testing.T) {
	expectedErr := crerrors.New("create failed")
	api := &fakeCloudflareAPI{
		createErr: expectedErr,
	}
	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     100,
	}

	_, err := client.CreateDNSRecord(context.Background(), "example.com", "A", "192.168.1.1")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestDNSClientDeleteError tests error handling in DeleteDNSRecord
func TestDNSClientDeleteError(t *testing.T) {
	expectedErr := crerrors.New("delete failed")
	api := &fakeCloudflareAPI{
		deleteErr: expectedErr,
	}
	client := &DNSClient{
		api:    api,
		zoneID: "zone",
	}

	err := client.DeleteDNSRecord(context.Background(), "record-1")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestDNSClientListError tests error handling in GetDNSRecords
func TestDNSClientListError(t *testing.T) {
	expectedErr := crerrors.New("list failed")
	api := &fakeCloudflareAPI{
		listErr: expectedErr,
	}
	client := &DNSClient{
		api:    api,
		zoneID: "zone",
	}

	_, err := client.GetDNSRecords(context.Background(), "example.com", "A")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestNewDNSClient tests the NewDNSClient constructor
func TestNewDNSClient(t *testing.T) {
	tests := []struct {
		name     string
		apiToken string
		zoneID   string
		proxied  bool
		ttl      int
	}{
		{
			name:     "create client with proxied enabled",
			apiToken: "test-token",
			zoneID:   "test-zone-id",
			proxied:  true,
			ttl:      300,
		},
		{
			name:     "create client with proxied disabled",
			apiToken: "test-token-2",
			zoneID:   "test-zone-id-2",
			proxied:  false,
			ttl:      120,
		},
		{
			name:     "create client with auto TTL (1)",
			apiToken: "test-token-3",
			zoneID:   "test-zone-id-3",
			proxied:  true,
			ttl:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewDNSClient(tt.apiToken, tt.zoneID, tt.proxied, tt.ttl)
			if err != nil {
				t.Fatalf("NewDNSClient() error = %v", err)
			}
			if client == nil {
				t.Fatal("NewDNSClient() returned nil client")
			}
			if client.zoneID != tt.zoneID {
				t.Errorf("zoneID = %v, want %v", client.zoneID, tt.zoneID)
			}
			if client.proxied != tt.proxied {
				t.Errorf("proxied = %v, want %v", client.proxied, tt.proxied)
			}
			if client.ttl != tt.ttl {
				t.Errorf("ttl = %v, want %v", client.ttl, tt.ttl)
			}
		})
	}
}

// TestGetZoneID tests the GetZoneID method
func TestGetZoneID(t *testing.T) {
	tests := []struct {
		name   string
		zoneID string
	}{
		{
			name:   "get zone ID",
			zoneID: "test-zone-123",
		},
		{
			name:   "get empty zone ID",
			zoneID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &DNSClient{
				zoneID: tt.zoneID,
			}
			got := client.GetZoneID()
			if got != tt.zoneID {
				t.Errorf("GetZoneID() = %v, want %v", got, tt.zoneID)
			}
		})
	}
}

// TestDNSClientReplaceRecordsMultiple tests ReplaceRecords with various scenarios
func TestDNSClientReplaceRecordsMultiple(t *testing.T) {
	tests := []struct {
		name          string
		existingCount int
		expectCreate  bool
		expectUpdate  bool
		expectDeletes int
		mockErr       error
		wantErr       bool
	}{
		{
			name:          "create when no records exist",
			existingCount: 0,
			expectCreate:  true,
			expectUpdate:  false,
			expectDeletes: 0,
		},
		{
			name:          "create new and delete existing record (atomic)",
			existingCount: 1,
			expectCreate:  true,
			expectUpdate:  false,
			expectDeletes: 1,
		},
		{
			name:          "create new and delete two existing records (atomic)",
			existingCount: 2,
			expectCreate:  true,
			expectUpdate:  false,
			expectDeletes: 2,
		},
		{
			name:          "create new and delete multiple records (atomic)",
			existingCount: 5,
			expectCreate:  true,
			expectUpdate:  false,
			expectDeletes: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var records []dns.RecordResponse
			for i := 0; i < tt.existingCount; i++ {
				records = append(records, dns.RecordResponse{
					ID:      fmt.Sprintf("record-%d", i),
					Name:    "example.com",
					Type:    dns.RecordResponseTypeA,
					Content: fmt.Sprintf("192.168.1.%d", i+1),
				})
			}

			api := &fakeCloudflareAPI{
				listResp: records,
			}
			client := &DNSClient{
				api:     api,
				zoneID:  "zone",
				proxied: false,
				ttl:     300,
			}

			err := client.ReplaceRecords(context.Background(), "example.com", "A", "192.168.1.100")

			if (err != nil) != tt.wantErr {
				t.Errorf("ReplaceRecords() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.expectCreate && len(api.createCalls) != 1 {
				t.Errorf("expected 1 create call, got %d", len(api.createCalls))
			}
			if !tt.expectCreate && len(api.createCalls) != 0 {
				t.Errorf("expected 0 create calls, got %d", len(api.createCalls))
			}

			if tt.expectUpdate && len(api.updateCalls) != 1 {
				t.Errorf("expected 1 update call, got %d", len(api.updateCalls))
			}
			if !tt.expectUpdate && len(api.updateCalls) != 0 {
				t.Errorf("expected 0 update calls, got %d", len(api.updateCalls))
			}

			if len(api.deleteCalls) != tt.expectDeletes {
				t.Errorf("expected %d delete calls, got %d", tt.expectDeletes, len(api.deleteCalls))
			}
		})
	}
}

// TestDNSClientWithDifferentTTLValues tests DNS operations with various TTL values
func TestDNSClientWithDifferentTTLValues(t *testing.T) {
	tests := []struct {
		name string
		ttl  int
	}{
		{name: "auto TTL", ttl: 1},
		{name: "60 seconds TTL", ttl: 60},
		{name: "300 seconds TTL", ttl: 300},
		{name: "3600 seconds TTL", ttl: 3600},
		{name: "86400 seconds TTL (1 day)", ttl: 86400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{}
			client := &DNSClient{
				api:     api,
				zoneID:  "zone",
				proxied: false,
				ttl:     tt.ttl,
			}

			_, err := client.CreateDNSRecord(context.Background(), "example.com", "A", "192.168.1.1")
			if err != nil {
				t.Errorf("CreateDNSRecord() with TTL %d error = %v", tt.ttl, err)
			}
		})
	}
}

// TestDNSClientProxiedSettings tests DNS operations with different proxied settings
func TestDNSClientProxiedSettings(t *testing.T) {
	tests := []struct {
		name    string
		proxied bool
	}{
		{name: "proxied enabled", proxied: true},
		{name: "proxied disabled", proxied: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{}
			client := &DNSClient{
				api:     api,
				zoneID:  "zone",
				proxied: tt.proxied,
				ttl:     300,
			}

			// Test Create
			_, err := client.CreateDNSRecord(context.Background(), "example.com", "A", "192.168.1.1")
			if err != nil {
				t.Errorf("CreateDNSRecord() with proxied=%v error = %v", tt.proxied, err)
			}

			// Test Update
			_, err = client.UpdateDNSRecord(context.Background(), "record-1", "example.com", "A", "192.168.1.2")
			if err != nil {
				t.Errorf("UpdateDNSRecord() with proxied=%v error = %v", tt.proxied, err)
			}
		})
	}
}

// TestDNSClientRecordTypes tests DNS operations with different record types
func TestDNSClientRecordTypes(t *testing.T) {
	tests := []struct {
		name       string
		recordType string
		content    string
	}{
		{name: "A record", recordType: "A", content: "192.168.1.1"},
		{name: "AAAA record", recordType: "AAAA", content: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{}
			client := &DNSClient{
				api:     api,
				zoneID:  "zone",
				proxied: false,
				ttl:     300,
			}

			// Test Create
			_, err := client.CreateDNSRecord(context.Background(), "example.com", tt.recordType, tt.content)
			if err != nil {
				t.Errorf("CreateDNSRecord() with type %s error = %v", tt.recordType, err)
			}

			// Test Update
			_, err = client.UpdateDNSRecord(context.Background(), "record-1", "example.com", tt.recordType, tt.content)
			if err != nil {
				t.Errorf("UpdateDNSRecord() with type %s error = %v", tt.recordType, err)
			}
		})
	}
}

// TestDNSClientGetDNSRecordsMultiple tests GetDNSRecords with multiple records
func TestDNSClientGetDNSRecordsMultiple(t *testing.T) {
	tests := []struct {
		name        string
		recordCount int
	}{
		{name: "no records", recordCount: 0},
		{name: "single record", recordCount: 1},
		{name: "two records", recordCount: 2},
		{name: "ten records", recordCount: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var records []dns.RecordResponse
			for i := 0; i < tt.recordCount; i++ {
				records = append(records, dns.RecordResponse{
					ID:      fmt.Sprintf("record-%d", i),
					Name:    "example.com",
					Type:    dns.RecordResponseTypeA,
					Content: fmt.Sprintf("192.168.1.%d", i+1),
				})
			}

			api := &fakeCloudflareAPI{
				listResp: records,
			}
			client := &DNSClient{
				api:    api,
				zoneID: "zone",
			}

			result, err := client.GetDNSRecords(context.Background(), "example.com", "A")
			if err != nil {
				t.Errorf("GetDNSRecords() error = %v", err)
			}

			if len(result) != tt.recordCount {
				t.Errorf("GetDNSRecords() returned %d records, want %d", len(result), tt.recordCount)
			}
		})
	}
}

// TestDNSClientReplaceRecordsListError tests error handling when listing fails
func TestDNSClientReplaceRecordsListError(t *testing.T) {
	expectedErr := crerrors.New("list failed")
	api := &fakeCloudflareAPI{
		listErr: expectedErr,
	}
	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     300,
	}

	err := client.ReplaceRecords(context.Background(), "example.com", "A", "192.168.1.1")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestDNSClientReplaceRecordsCreateError tests error handling when creation fails
func TestDNSClientReplaceRecordsCreateError(t *testing.T) {
	expectedErr := crerrors.New("create failed")
	api := &fakeCloudflareAPI{
		listResp:  []dns.RecordResponse{}, // No existing records
		createErr: expectedErr,
	}
	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     300,
	}

	err := client.ReplaceRecords(context.Background(), "example.com", "A", "192.168.1.1")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !crerrors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestDNSClientReplaceRecordsDeleteError tests error handling when deletion fails
func TestDNSClientReplaceRecordsDeleteError(t *testing.T) {
	expectedErr := crerrors.New("delete failed")
	api := &fakeCloudflareAPI{
		listResp: []dns.RecordResponse{
			{ID: "record-1", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "192.168.1.1"},
			{ID: "record-2", Name: "example.com", Type: dns.RecordResponseTypeA, Content: "192.168.1.2"},
		},
		deleteErr: expectedErr,
	}
	client := &DNSClient{
		api:     api,
		zoneID:  "zone",
		proxied: false,
		ttl:     300,
	}

	err := client.ReplaceRecords(context.Background(), "example.com", "A", "192.168.1.100")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	// Now deleteRecords continues deleting even if some fail, returning an aggregate error
	// So we check that the error message contains the original error message
	if !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected error to contain 'delete failed', got %v", err)
	}
}
