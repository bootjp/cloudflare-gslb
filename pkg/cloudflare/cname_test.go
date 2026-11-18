package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
)

func TestReplaceCNAMEUpdatesExistingID(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"existing","type":"CNAME","name":"app.example.com","content":"old.cfargotunnel.com","proxied":true}],"result_info":{"page":1,"per_page":100,"total_pages":1,"total_count":1}}`))
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/zones/zone/dns_records/existing" {
			t.Errorf("destructive or conflicting operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["type"] != "CNAME" || body["content"] != "new.cfargotunnel.com" || body["proxied"] != true {
			t.Errorf("wrong record body: %v", body)
		}
		writes++
		_, _ = w.Write([]byte(`{"success":true,"result":{"id":"existing","type":"CNAME","name":"app.example.com","content":"new.cfargotunnel.com","proxied":true}}`))
	}))
	defer srv.Close()
	api := cf.NewClient(option.WithAPIToken("test"), option.WithBaseURL(srv.URL+"/"))
	c := &DNSClient{api: api.DNS.Records, zoneID: "zone", proxied: true, ttl: 60}
	if err := c.ReplaceRecords(context.Background(), "app.example.com", "CNAME", []string{"new.cfargotunnel.com"}); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("writes=%d", writes)
	}
}

func TestRejectInvalidCNAMEWritesBeforeAPICall(t *testing.T) {
	f := &fakeCloudflareAPI{listResp: []dns.RecordResponse{}}
	c := &DNSClient{api: f, zoneID: "zone", ttl: 60}
	if err := c.ReplaceRecords(context.Background(), "app.example.com", "CNAME", []string{"one.example.com", "two.example.com"}); err == nil {
		t.Fatal("accepted multiple CNAME targets")
	}
	if _, err := c.CreateDNSRecord(context.Background(), "app.example.com", "TXT", "value"); err == nil {
		t.Fatal("unknown record type fell back to A")
	}
	if _, err := c.UpdateDNSRecord(context.Background(), "id", "app.example.com", "TXT", "value"); err == nil {
		t.Fatal("unknown update type fell back to A")
	}
	if len(f.createCalls)+len(f.updateCalls)+len(f.deleteCalls) != 0 {
		t.Fatal("unexpected mutation")
	}
}
