package main

import (
	"context"
	"flag"
	"log"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/gslb"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Probe without updating DNS or sending failover notifications")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cfg.DryRun = cfg.DryRun || *dryRun
	service, err := gslb.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to create GSLB service: %v", err)
	}

	log.Println("Running one-shot health check...")
	ctx := context.Background()

	if err := service.RunOneShot(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	log.Println("One-shot health check completed successfully")
}
