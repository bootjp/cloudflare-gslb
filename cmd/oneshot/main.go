package main

import (
	"context"
	"flag"
	"log"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/gslb"
)

func main() {
	// コマンドライン引数の処理
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	// 設定ファイルの読み込み
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// サービスの作成
	service, err := gslb.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to create GSLB service: %v", err)
	}

	// oneshotモードでのヘルスチェック実行
	log.Println("Running one-shot health check...")
	ctx := context.Background()

	// ヘルスチェックの実行
	if err := service.RunOneShot(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	log.Println("One-shot health check completed successfully")
}
