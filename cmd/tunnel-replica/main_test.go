package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMultipleProbesRequireBothApplications(t *testing.T) {
	var failed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Host == "misskey.example.com" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/heartbeat" && r.Host == "umami.example.com" && !failed.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := replicaConfig{
		Probes: []replicaProbe{
			{URL: server.URL + "/healthz", Host: "misskey.example.com"},
			{URL: server.URL + "/api/heartbeat", Host: "umami.example.com"},
		},
		Timeout: 2, Interval: 2, Fall: 2, Rise: 2,
		CloudflaredPath: "/usr/local/bin/cloudflared", TokenFile: "/tmp/token", MetricsAddress: "127.0.0.1:2021",
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.checkProbes(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed.Store(true)
	if err := cfg.checkProbes(context.Background()); err == nil {
		t.Fatal("expected unhealthy Umami to make shared replica unhealthy")
	}
}
