package cloudflare

import (
	"context"
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
		name:    "",  // Would need type assertion to extract
		rtype:   "",  // Would need type assertion to extract
		content: "",  // Would need type assertion to extract
		ttl:     0,   // Would need type assertion to extract
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

	if len(api.updateCalls) != 1 {
		t.Fatalf("expected update to be called once, got %d", len(api.updateCalls))
	}

	update := api.updateCalls[0]
	if update.recordID != "record-1" {
		t.Fatalf("expected record ID record-1, got %s", update.recordID)
	}

	if len(api.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls, got %d", len(api.deleteCalls))
	}
	if len(api.createCalls) != 0 {
		t.Fatalf("expected no create calls, got %d", len(api.createCalls))
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
	if time.Since(start) < 500*time.Millisecond {
		t.Fatalf("expected deletion to respect delay between operations")
	}

	if len(api.updateCalls) != 1 {
		t.Fatalf("expected one update call, got %d", len(api.updateCalls))
	}
	if len(api.deleteCalls) != 1 {
		t.Fatalf("expected one delete call, got %d", len(api.deleteCalls))
	}
	if api.deleteCalls[0] != "record-2" {
		t.Fatalf("expected record-2 to be deleted, got %s", api.deleteCalls[0])
	}
	if len(api.createCalls) != 0 {
		t.Fatalf("expected no create calls, got %d", len(api.createCalls))
	}
}

func TestDNSClientReplaceRecordsUpdateError(t *testing.T) {
	expected := crerrors.New("update failed")
	api := &fakeCloudflareAPI{
		listResp:  []dns.RecordResponse{{ID: "record-1", Name: "example.com", Type: dns.RecordResponseTypeA, Proxied: false}},
		updateErr: expected,
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
	if len(api.createCalls) != 0 {
		t.Fatalf("expected no create calls on error, got %d", len(api.createCalls))
	}
}
