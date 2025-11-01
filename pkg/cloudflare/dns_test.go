package cloudflare

import (
	"context"
	"testing"
	"time"

	cf "github.com/cloudflare/cloudflare-go"
	crerrors "github.com/cockroachdb/errors"
)

type fakeCloudflareAPI struct {
	listResp    []cf.DNSRecord
	listErr     error
	createCalls []cf.CreateDNSRecordParams
	updateCalls []cf.UpdateDNSRecordParams
	deleteCalls []string
	createErr   error
	updateErr   error
	deleteErr   error
}

func (f *fakeCloudflareAPI) ListDNSRecords(ctx context.Context, rc *cf.ResourceContainer, params cf.ListDNSRecordsParams) ([]cf.DNSRecord, *cf.ResultInfo, error) {
	if f.listErr != nil {
		return nil, nil, f.listErr
	}
	records := make([]cf.DNSRecord, len(f.listResp))
	copy(records, f.listResp)
	return records, &cf.ResultInfo{}, nil
}

func (f *fakeCloudflareAPI) CreateDNSRecord(ctx context.Context, rc *cf.ResourceContainer, params cf.CreateDNSRecordParams) (cf.DNSRecord, error) {
	f.createCalls = append(f.createCalls, params)
	if f.createErr != nil {
		return cf.DNSRecord{}, f.createErr
	}
	return cf.DNSRecord{
		ID:       "created",
		Name:     params.Name,
		Type:     params.Type,
		Content:  params.Content,
		TTL:      params.TTL,
		Proxied:  params.Proxied,
		Priority: params.Priority,
	}, nil
}

func (f *fakeCloudflareAPI) UpdateDNSRecord(ctx context.Context, rc *cf.ResourceContainer, params cf.UpdateDNSRecordParams) (cf.DNSRecord, error) {
	f.updateCalls = append(f.updateCalls, params)
	if f.updateErr != nil {
		return cf.DNSRecord{}, f.updateErr
	}
	return cf.DNSRecord{
		ID:       params.ID,
		Name:     params.Name,
		Type:     params.Type,
		Content:  params.Content,
		TTL:      params.TTL,
		Proxied:  params.Proxied,
		Priority: params.Priority,
	}, nil
}

func (f *fakeCloudflareAPI) DeleteDNSRecord(ctx context.Context, rc *cf.ResourceContainer, recordID string) error {
	f.deleteCalls = append(f.deleteCalls, recordID)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

func TestDNSClientReplaceRecordsCreatesWhenNoRecords(t *testing.T) {
	api := &fakeCloudflareAPI{}
	client := &DNSClient{
		api:      api,
		zoneID:   "zone",
		proxied:  true,
		ttl:      120,
		priority: 5,
	}

	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.10"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(api.createCalls) != 1 {
		t.Fatalf("expected create to be called once, got %d", len(api.createCalls))
	}

	params := api.createCalls[0]
	if params.Name != "example.com" || params.Type != "A" || params.Content != "203.0.113.10" {
		t.Fatalf("unexpected create params: %+v", params)
	}
	if params.Proxied == nil || !*params.Proxied {
		t.Fatalf("expected proxied flag to be true: %+v", params)
	}
	if params.Priority == nil || *params.Priority != uint16(5) {
		t.Fatalf("expected priority 5, got %+v", params.Priority)
	}
	if params.TTL != 120 {
		t.Fatalf("expected TTL 120, got %d", params.TTL)
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
		listResp: []cf.DNSRecord{{
			ID:      "record-1",
			Name:    "example.com",
			Type:    "A",
			Content: "198.51.100.1",
		}},
	}

	client := &DNSClient{
		api:      api,
		zoneID:   "zone",
		proxied:  false,
		ttl:      300,
		priority: 0,
	}

	if err := client.ReplaceRecords(context.Background(), "example.com", "A", "203.0.113.20"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(api.updateCalls) != 1 {
		t.Fatalf("expected update to be called once, got %d", len(api.updateCalls))
	}

	update := api.updateCalls[0]
	if update.ID != "record-1" {
		t.Fatalf("expected record ID record-1, got %s", update.ID)
	}
	if update.Content != "203.0.113.20" {
		t.Fatalf("expected updated content, got %s", update.Content)
	}
	if update.Name != "example.com" || update.Type != "A" {
		t.Fatalf("unexpected update params: %+v", update)
	}
	if update.Proxied == nil || *update.Proxied {
		t.Fatalf("expected proxied flag to be false: %+v", update)
	}
	if update.Priority == nil || *update.Priority != uint16(0) {
		t.Fatalf("expected priority 0, got %+v", update.Priority)
	}
	if update.TTL != 300 {
		t.Fatalf("expected TTL 300, got %d", update.TTL)
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
		listResp: []cf.DNSRecord{
			{ID: "record-1", Name: "example.com", Type: "A", Content: "198.51.100.1"},
			{ID: "record-2", Name: "example.com", Type: "A", Content: "198.51.100.2"},
		},
	}

	client := &DNSClient{
		api:      api,
		zoneID:   "zone",
		proxied:  true,
		ttl:      60,
		priority: 1,
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
		listResp:  []cf.DNSRecord{{ID: "record-1", Name: "example.com", Type: "A"}},
		updateErr: expected,
	}

	client := &DNSClient{
		api:      api,
		zoneID:   "zone",
		proxied:  false,
		ttl:      100,
		priority: 2,
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
