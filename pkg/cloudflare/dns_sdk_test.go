package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	cf "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/option"
)

func TestDNSRecordSDKRequests(t *testing.T) {
	for recordType, content := range map[string]string{
		"A": "192.0.2.1", "AAAA": "2001:db8::1", "CNAME": "target.example.net",
	} {
		for _, proxied := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/proxied=%t", recordType, proxied), func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing API token header")
					}
					path := "/zones/test-zone/dns_records"
					if r.Method == http.MethodPut {
						path += "/record-id"
					}
					if r.URL.Path != path {
						t.Errorf("path=%s, want %s", r.URL.Path, path)
					}
					w.Header().Set("Content-Type", "application/json")
					record := map[string]any{"id": "record-id", "name": "app.example.com", "type": recordType, "content": content, "ttl": 300, "proxied": proxied}
					switch r.Method {
					case http.MethodGet:
						if r.URL.Query().Get("name.exact") != "app.example.com" || r.URL.Query().Get("type") != recordType {
							t.Errorf("unexpected list filters: %s", r.URL.RawQuery)
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{record}, "result_info": map[string]int{"page": 1, "total_pages": 1}})
						return
					case http.MethodPost, http.MethodPut:
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						for key, want := range record {
							if key == "id" {
								continue
							}
							if key == "ttl" {
								want = float64(300)
							}
							if body[key] != want {
								t.Errorf("%s: %s=%v, want %v", r.Method, key, body[key], want)
							}
						}
					default:
						t.Errorf("unexpected method: %s", r.Method)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": record})
				}))
				defer srv.Close()
				api := cf.NewClient(option.WithAPIToken("test-token"), option.WithBaseURL(srv.URL+"/"))
				client := &DNSClient{api: api.DNS.Records, zoneID: "test-zone", proxied: proxied, ttl: 300}
				ctx := context.Background()
				created, err := client.CreateDNSRecord(ctx, "app.example.com", recordType, content)
				if err != nil || created.Content != content || created.Proxied != proxied {
					t.Fatalf("create=%+v, err=%v", created, err)
				}
				updated, err := client.UpdateDNSRecord(ctx, "record-id", "app.example.com", recordType, content)
				if err != nil || updated.Content != content || updated.Proxied != proxied {
					t.Fatalf("update=%+v, err=%v", updated, err)
				}
				records, err := client.GetDNSRecords(ctx, "app.example.com", recordType)
				if err != nil || len(records) != 1 || records[0].Content != content || records[0].Proxied != proxied {
					t.Fatalf("list=%+v, err=%v", records, err)
				}
				if requests != 3 {
					t.Fatalf("requests=%d, want 3", requests)
				}
			})
		}
	}
}
