package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/gslb"
)

func main() {
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	service, err := gslb.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to create GSLB service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	if err := service.Start(ctx); err != nil {
		log.Printf("Failed to start GSLB service: %v", err)
		return
	}

	sig := <-signalCh
	log.Printf("Received signal: %v", sig)

	service.Stop()
}
