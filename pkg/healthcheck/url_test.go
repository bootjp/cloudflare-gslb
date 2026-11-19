package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestURLProbeRejectsRedirectWithoutFollowing(t *testing.T) {
	targetHit := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		targetHit = true
	}))
	defer s.Close()
	c := &URLChecker{Timeout: time.Second}
	if err := c.Check(s.URL + "/healthz"); err == nil {
		t.Fatal("redirect accepted as healthy")
	}
	if targetHit {
		t.Fatal("probe followed a redirect")
	}
}

func TestURLProbePreservesLocalHostAndHeaders(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "app.example.com" || r.Header.Get("X-Healthcheck") != "test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()
	c := &URLChecker{Timeout: time.Second, Host: "app.example.com", Headers: map[string]string{"X-Healthcheck": "test"}}
	if err := c.Check(s.URL + "/healthz"); err != nil {
		t.Fatal(err)
	}
}

func TestURLProbeVerifiesTLSAndCancels(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer s.Close()
	c := &URLChecker{Timeout: time.Second}
	if err := c.Check(s.URL); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.CheckContext(ctx, s.URL); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}
