package healthcheck

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
)

func TestNewChecker(t *testing.T) {
	tests := []struct {
		name    string
		hc      config.HealthCheck
		wantErr bool
	}{
		{
			name: "HTTP Checker",
			hc: config.HealthCheck{
				Type:     "http",
				Endpoint: "/health",
				Host:     "example.com",
				Timeout:  5,
			},
			wantErr: false,
		},
		{
			name: "HTTPS Checker",
			hc: config.HealthCheck{
				Type:     "https",
				Endpoint: "/health",
				Host:     "example.com",
				Timeout:  5,
			},
			wantErr: false,
		},
		{
			name: "HTTPS Checker with InsecureSkipVerify",
			hc: config.HealthCheck{
				Type:               "https",
				Endpoint:           "/health",
				Host:               "example.com",
				Timeout:            5,
				InsecureSkipVerify: true,
			},
			wantErr: false,
		},
		{
			name: "ICMP Checker",
			hc: config.HealthCheck{
				Type:    "icmp",
				Timeout: 5,
			},
			wantErr: false,
		},
		{
			name: "Unknown Checker Type",
			hc: config.HealthCheck{
				Type:    "unknown",
				Timeout: 5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewChecker(tt.hc)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Errorf("NewChecker() returned nil, expected non-nil")
			}
		})
	}
}

func TestHttpChecker_Check(t *testing.T) {
	// テスト用のHTTPサーバを設定
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// サーバのアドレスからホスト部分を抽出
	host := server.URL[7:] // "http://" を取り除く

	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{
			name:     "Success",
			endpoint: "/health",
			wantErr:  false,
		},
		{
			name:     "Error Status",
			endpoint: "/error",
			wantErr:  true,
		},
		{
			name:     "Not Found",
			endpoint: "/notfound",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HttpChecker{
				Endpoint: tt.endpoint,
				Timeout:  5 * time.Second,
				Scheme:   "http",
			}
			if err := h.Check(host); (err != nil) != tt.wantErr {
				t.Errorf("HttpChecker.Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHttpChecker_CheckWithHeaders(t *testing.T) {
	headerCh := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerCh <- r.Header.Get("X-Test-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host := server.URL[7:]

	h := &HttpChecker{
		Endpoint: "/health",
		Timeout:  5 * time.Second,
		Scheme:   "http",
		Headers: map[string]string{
			"X-Test-Header": "expected-value",
		},
	}

	if err := h.Check(host); err != nil {
		t.Fatalf("HttpChecker.Check() error = %v", err)
	}

	select {
	case headerValue := <-headerCh:
		if headerValue != "expected-value" {
			t.Errorf("Expected header X-Test-Header = 'expected-value', got '%s'", headerValue)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for header to be received")
	}
}

// ICMPのテストは実行環境に依存するため、ここでは省略しています。
// 実際の環境でテストする場合は、以下のように実装できます。
/*
func TestIcmpChecker_Check(t *testing.T) {
	// ローカルホストに対してICMPチェックを行う
	checker := &IcmpChecker{
		Timeout: 5 * time.Second,
	}

	// localhostに対してチェック
	err := checker.Check("127.0.0.1")
	if err != nil {
		t.Errorf("IcmpChecker.Check() error = %v", err)
	}

	// 存在しないIPアドレスに対してチェック
	err = checker.Check("192.0.2.1") // TEST-NET-1 (RFC 5737)
	if err == nil {
		t.Errorf("IcmpChecker.Check() expected error for non-existent IP")
	}
}
*/
