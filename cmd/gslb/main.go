package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/gslb"
)

func main() {
	// コマンドライン引数の解析
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	// 設定ファイルの読み込み
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// GSLBサービスの作成
	service, err := gslb.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to create GSLB service: %v", err)
	}

	// シグナルハンドラの設定
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// シグナルの受信チャネル
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	// サービスの開始
	if err := service.Start(ctx); err != nil {
		log.Fatalf("Failed to start GSLB service: %v", err)
	}

	// シグナルを待機
	sig := <-signalCh
	log.Printf("Received signal: %v", sig)

	// サービスの停止
	service.Stop()
}
