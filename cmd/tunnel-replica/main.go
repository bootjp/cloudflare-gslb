package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bootjp/cloudflare-gslb/pkg/healthcheck"
	"github.com/bootjp/cloudflare-gslb/pkg/replica"
)

type replicaConfig struct {
	ProbeURL        string            `json:"probe_url"`
	Host            string            `json:"host"`
	Headers         map[string]string `json:"headers"`
	Probes          []replicaProbe    `json:"probes"`
	Timeout         int               `json:"timeout_seconds"`
	Interval        int               `json:"interval_seconds"`
	Fall            int               `json:"fall"`
	Rise            int               `json:"rise"`
	CloudflaredPath string            `json:"cloudflared_path"`
	TokenFile       string            `json:"token_file"`
	MetricsAddress  string            `json:"metrics_address"`
}

type replicaProbe struct {
	URL     string            `json:"url"`
	Host    string            `json:"host"`
	Headers map[string]string `json:"headers"`
}

func loadReplicaConfig(path string) (replicaConfig, error) {
	var cfg replicaConfig
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func main() {
	path := flag.String("config", "replica.json", "Path to operator-owned replica configuration")
	validate := flag.Bool("validate", false, "Validate configuration without probing or launching a connector")
	flag.Parse()
	cfg, err := loadReplicaConfig(*path)
	if err != nil {
		log.Fatal(err)
	}
	if *validate {
		log.Print("Replica configuration valid")
		return
	}
	if err := runReplica(cfg); err != nil {
		log.Fatal(err)
	}
}

func runReplica(cfg replicaConfig) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	process := &replica.Process{Path: cfg.CloudflaredPath, Args: []string{"tunnel", "--no-autoupdate", "--metrics", cfg.MetricsAddress, "run", "--token-file", cfg.TokenFile}}
	gate := &replica.Gate{Probe: cfg.checkProbes, Connector: process, Interval: time.Duration(cfg.Interval) * time.Second, Fall: cfg.Fall, Rise: cfg.Rise}
	return gate.Run(ctx)
}

func (cfg replicaConfig) checkProbes(ctx context.Context) error {
	probes := cfg.Probes
	if len(probes) == 0 {
		probes = []replicaProbe{{URL: cfg.ProbeURL, Host: cfg.Host, Headers: cfg.Headers}}
	}
	for _, probe := range probes {
		checker := &healthcheck.URLChecker{Timeout: time.Duration(cfg.Timeout) * time.Second, Headers: probe.Headers, Host: probe.Host}
		if err := checker.CheckContext(ctx, probe.URL); err != nil {
			return fmt.Errorf("probe %s failed: %w", probe.URL, err)
		}
	}
	return nil
}

func (cfg replicaConfig) validate() error {
	if err := cfg.validateProbes(); err != nil {
		return err
	}
	if cfg.Interval < 1 || cfg.Interval > 300 || cfg.Timeout < 1 || cfg.Timeout > 300 {
		return fmt.Errorf("interval and timeout must be 1..300 seconds")
	}
	if cfg.Fall < 1 || cfg.Rise < 1 {
		return fmt.Errorf("positive fall/rise required")
	}
	if !filepath.IsAbs(cfg.CloudflaredPath) || !filepath.IsAbs(cfg.TokenFile) {
		return fmt.Errorf("cloudflared and token_file must use absolute paths")
	}
	return validateMetrics(cfg.MetricsAddress)
}

func (cfg replicaConfig) validateProbes() error {
	if len(cfg.Probes) > 0 {
		if cfg.ProbeURL != "" || cfg.Host != "" || len(cfg.Headers) > 0 {
			return fmt.Errorf("use either probes or probe_url, host and headers")
		}
		for _, probe := range cfg.Probes {
			if err := validateProbeURL(probe.URL); err != nil {
				return err
			}
		}
	} else if err := validateProbeURL(cfg.ProbeURL); err != nil {
		return err
	}
	return nil
}
func validateProbeURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid independent origin probe URL")
	}
	return nil
}
func validateMetrics(address string) error {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || port == "" {
		return fmt.Errorf("metrics_address must bind a loopback IP and port")
	}
	return nil
}
