package gslb

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	"github.com/bootjp/cloudflare-gslb/pkg/healthcheck"
	cf "github.com/cloudflare/cloudflare-go"
)

// OriginStatus はオリジンの現在の状態を表す構造体
type OriginStatus struct {
	CurrentIP       string
	UsingPriority   bool
	HealthyPriority bool
	LastCheck       time.Time
}

// Service はGSLBサービスを表す構造体
type Service struct {
	config     *config.Config
	dnsClient  cloudflare.DNSClientInterface
	checkMutex sync.Mutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
	// オリジンごとにフェイルオーバーIPのインデックスを管理
	failoverIndices map[string]int
	// オリジンごとのDNSクライアント
	dnsClients map[string]cloudflare.DNSClientInterface
	// オリジンごとの状態を管理
	originStatus map[string]*OriginStatus
}

// NewService は新しいGSLBサービスを作成する
func NewService(cfg *config.Config) (*Service, error) {
	// デフォルトのDNSクライアントを初期化（後方互換性のため）
	defaultClient, err := cloudflare.NewDNSClient(
		cfg.CloudflareAPIToken,
		cfg.CloudflareZoneID,
		false, // デフォルトはプロキシしない
		60,    // TTL: 60秒
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize default DNS client: %w", err)
	}

	// 各オリジンごとのDNSクライアントを作成
	dnsClients := make(map[string]cloudflare.DNSClientInterface)

	// オリジンごとに個別のDNSクライアントを作成
	for _, origin := range cfg.Origins {
		originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)
		client, err := cloudflare.NewDNSClient(
			cfg.CloudflareAPIToken,
			cfg.CloudflareZoneID,
			origin.Proxied, // オリジンごとのプロキシ設定
			60,             // TTL: 60秒
		)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize DNS client for %s: %w", originKey, err)
		}
		dnsClients[originKey] = client
	}

	return &Service{
		config:          cfg,
		dnsClient:       defaultClient,
		stopCh:          make(chan struct{}),
		failoverIndices: make(map[string]int),
		dnsClients:      dnsClients,
		originStatus:    make(map[string]*OriginStatus),
	}, nil
}

// getDNSClientForOrigin はオリジン用のDNSクライアントを取得する
func (s *Service) getDNSClientForOrigin(origin config.OriginConfig) cloudflare.DNSClientInterface {
	originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)
	client, exists := s.dnsClients[originKey]
	if !exists {
		// クライアントが見つからない場合はデフォルトを使用
		return s.dnsClient
	}
	return client
}

// Start はGSLBサービスを開始する
func (s *Service) Start(ctx context.Context) error {
	log.Println("Starting GSLB service...")

	// すべてのオリジンに対してチェックを開始
	for _, origin := range s.config.Origins {
		s.wg.Add(1)
		go s.monitorOrigin(ctx, origin)
	}

	return nil
}

// Stop はGSLBサービスを停止する
func (s *Service) Stop() {
	log.Println("Stopping GSLB service...")
	close(s.stopCh)
	s.wg.Wait()
	log.Println("GSLB service stopped")
}

// monitorOrigin はオリジンをモニタリングする
func (s *Service) monitorOrigin(ctx context.Context, origin config.OriginConfig) {
	defer s.wg.Done()

	log.Printf("Starting monitoring for origin: %s (%s)", origin.Name, origin.RecordType)

	// ヘルスチェッカーを作成
	checker, err := healthcheck.NewChecker(origin.HealthCheck)
	if err != nil {
		log.Printf("Failed to create health checker for %s: %v", origin.Name, err)
		return
	}

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	// オリジンのキーを生成
	originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)

	// 状態の初期化（なければ）
	if _, exists := s.originStatus[originKey]; !exists {
		// 優先IPがあれば初期状態を設定（ただし実際のDNS設定が不明なので仮の設定）
		initialUsingPriority := len(origin.PriorityFailoverIPs) > 0
		log.Printf("Initializing state for %s: initialUsingPriority=%t (will be verified on first check)",
			origin.Name, initialUsingPriority)

		s.originStatus[originKey] = &OriginStatus{
			UsingPriority:   initialUsingPriority, // 優先IPがあれば初期状態は優先IP（後で実際の状態と同期される）
			HealthyPriority: true,                 // 初期状態では優先IPは健全と仮定
		}
	}

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Printf("Running check cycle for origin: %s (%s)", origin.Name, origin.RecordType)
			s.checkOrigin(ctx, origin, checker)
			// 優先IPのヘルスチェック（優先IPに戻るオプションが有効な場合）
			if origin.ReturnToPriority && len(origin.PriorityFailoverIPs) > 0 {
				log.Printf("ReturnToPriority is enabled, checking priority IPs for %s", origin.Name)
				s.checkPriorityIPs(ctx, origin, checker)
			} else {
				log.Printf("ReturnToPriority is disabled or no priority IPs for %s", origin.Name)
			}
		}
	}
}

// checkPriorityIPs は優先IPのヘルスチェックを実行する
func (s *Service) checkPriorityIPs(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) {
	originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)
	status := s.originStatus[originKey]

	log.Printf("Checking priority IPs for %s, current status: UsingPriority=%t, HealthyPriority=%t, CurrentIP=%s",
		origin.Name, status.UsingPriority, status.HealthyPriority, status.CurrentIP)

	// 実際のIPが優先IPリストにあるかどうかをチェック
	isPriorityIP := false
	for _, priorityIP := range origin.PriorityFailoverIPs {
		if status.CurrentIP == priorityIP {
			isPriorityIP = true
			break
		}
	}

	// 現在のIPと状態を同期
	if isPriorityIP != status.UsingPriority {
		log.Printf("Fixing inconsistent state for %s: UsingPriority=%t but current IP %s is %s a priority IP",
			origin.Name, status.UsingPriority, status.CurrentIP,
			map[bool]string{true: "actually", false: "not"}[isPriorityIP])
		status.UsingPriority = isPriorityIP
	}

	// 既に優先IPを使用している場合はスキップ
	if status.UsingPriority {
		log.Printf("Already using priority IP for %s, skipping check", origin.Name)
		return
	}

	// 優先IPのヘルスチェック
	allHealthy := true
	for _, ip := range origin.PriorityFailoverIPs {
		if err := checker.Check(ip); err != nil {
			log.Printf("Priority IP %s is still unhealthy: %v", ip, err)
			allHealthy = false
			break
		}
		log.Printf("Priority IP %s is healthy", ip)
	}

	// 優先IPが健全になったら切り替え
	if allHealthy {
		log.Printf("Priority IPs for %s are now healthy, switching back", origin.Name)
		status.HealthyPriority = true

		// 優先IPの最初のIPに切り替え
		if len(origin.PriorityFailoverIPs) > 0 {
			dnsClient := s.getDNSClientForOrigin(origin)
			newIP := origin.PriorityFailoverIPs[0]

			// DNSレコードを更新
			if err := dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP); err != nil {
				log.Printf("Failed to switch back to priority IP for %s: %v", origin.Name, err)
				return
			}

			// 状態を更新
			status.UsingPriority = true
			status.CurrentIP = newIP
			status.LastCheck = time.Now()
			log.Printf("Switched back to priority IP %s for %s", newIP, origin.Name)
		}
	}
}

// checkOrigin はオリジンのヘルスチェックを実行する
func (s *Service) checkOrigin(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) {
	// 同時に複数のチェックが走らないようにロックを取得
	s.checkMutex.Lock()
	defer s.checkMutex.Unlock()

	log.Printf("Checking origin: %s (%s)", origin.Name, origin.RecordType)

	// このオリジン用のDNSクライアントを取得
	dnsClient := s.getDNSClientForOrigin(origin)

	// 現在のDNSレコードを取得
	records, err := dnsClient.GetDNSRecords(ctx, origin.Name, origin.RecordType)
	if err != nil {
		log.Printf("Failed to get DNS records for %s: %v", origin.Name, err)
		return
	}

	// レコードが存在しない場合はログを出して終了
	if len(records) == 0 {
		log.Printf("No DNS records found for %s", origin.Name)
		return
	}

	// オリジンの状態を取得または初期化
	originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)
	status := s.getOrInitOriginStatus(originKey)

	// 各レコードに対してヘルスチェックを実行
	for _, record := range records {
		s.processRecord(ctx, origin, record, checker, status)
	}
}

// getOrInitOriginStatus はオリジンの状態を取得または初期化する
func (s *Service) getOrInitOriginStatus(originKey string) *OriginStatus {
	status, exists := s.originStatus[originKey]
	if !exists {
		status = &OriginStatus{
			UsingPriority:   false,
			HealthyPriority: true,
		}
		s.originStatus[originKey] = status
	}
	return status
}

// processRecord は各レコードに対してヘルスチェックを実行し結果を処理する
func (s *Service) processRecord(ctx context.Context, origin config.OriginConfig, record cf.DNSRecord, checker healthcheck.Checker, status *OriginStatus) {
	// IPアドレスを取得
	ip := record.Content
	status.CurrentIP = ip

	// ヘルスチェックを実行
	err := checker.Check(ip)
	if err != nil {
		log.Printf("Health check failed for %s (%s): %v", origin.Name, ip, err)

		// 優先IPを使用中に障害が発生した場合
		if status.UsingPriority && len(origin.PriorityFailoverIPs) > 0 {
			status.HealthyPriority = false
			status.UsingPriority = false
		}

		// 新しいレコードを見つけて置き換え
		if err := s.replaceUnhealthyRecord(ctx, origin, record); err != nil {
			log.Printf("Failed to replace unhealthy record for %s: %v", origin.Name, err)
		}
	} else {
		log.Printf("Health check passed for %s (%s)", origin.Name, ip)

		// 正常なIPが優先IPリストにあるかどうかをチェック
		isPriorityIP := false
		for _, priorityIP := range origin.PriorityFailoverIPs {
			if ip == priorityIP {
				isPriorityIP = true
				break
			}
		}

		// 状態を更新
		status.UsingPriority = isPriorityIP
		status.CurrentIP = ip
		status.LastCheck = time.Now()
	}
}

// replaceUnhealthyRecord は異常なレコードを新しいレコードに置き換える
func (s *Service) replaceUnhealthyRecord(ctx context.Context, origin config.OriginConfig, unhealthyRecord cf.DNSRecord) error {
	// オリジンのキーを生成（名前とレコードタイプの組み合わせ）
	originKey := fmt.Sprintf("%s-%s", origin.Name, origin.RecordType)

	// このオリジン用のDNSクライアントを取得
	dnsClient := s.getDNSClientForOrigin(origin)

	// 状態を取得
	status := s.originStatus[originKey]

	// 現在優先IPを使用中で、優先IPが異常な場合は通常のフェイルオーバーIPに切り替え
	if status.UsingPriority && !status.HealthyPriority && len(origin.FailoverIPs) > 0 {
		return s.switchToPrimaryFailover(ctx, origin, dnsClient, originKey, status)
	}

	// フェイルオーバーIPが設定されている場合
	if len(origin.FailoverIPs) > 0 {
		return s.useNextFailoverIP(ctx, origin, unhealthyRecord, dnsClient, originKey)
	}

	// フェイルオーバーIPが設定されていない場合は、自動生成したIPを使用
	return s.useAutoGeneratedIP(ctx, origin, unhealthyRecord, dnsClient)
}

// switchToPrimaryFailover は優先IPから通常のフェイルオーバーIPに切り替える
func (s *Service) switchToPrimaryFailover(ctx context.Context, origin config.OriginConfig, dnsClient cloudflare.DNSClientInterface, originKey string, status *OriginStatus) error {
	status.UsingPriority = false

	// 通常のフェイルオーバーIPの最初のIPに切り替え
	newIP := origin.FailoverIPs[0]
	s.failoverIndices[originKey] = 0

	log.Printf("Switching from priority IP to regular failover IP: %s for %s",
		newIP, origin.Name)
	return dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP)
}

// useNextFailoverIP は次のフェイルオーバーIPを使用する
func (s *Service) useNextFailoverIP(ctx context.Context, origin config.OriginConfig, unhealthyRecord cf.DNSRecord, dnsClient cloudflare.DNSClientInterface, originKey string) error {
	// 現在のインデックスを取得（初めての場合は0）
	currentIndex, exists := s.failoverIndices[originKey]
	if !exists {
		currentIndex = 0
	}

	// 次のインデックスを計算（循環させる）
	nextIndex := (currentIndex + 1) % len(origin.FailoverIPs)
	s.failoverIndices[originKey] = nextIndex

	// フェイルオーバーIPを取得
	newIP := origin.FailoverIPs[nextIndex]

	// IPタイプをチェック
	if err := s.validateIPType(origin.RecordType, newIP); err != nil {
		return err
	}

	// 既存のレコードを削除して新しいレコードを作成
	log.Printf("Replacing unhealthy record %s with failover IP: %s (index: %d, proxied: %t)",
		unhealthyRecord.Content, newIP, nextIndex, origin.Proxied)
	return dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP)
}

// validateIPType はIPアドレスがレコードタイプに合っているかを検証する
func (s *Service) validateIPType(recordType, ipAddress string) error {
	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", ipAddress)
	}

	if recordType == "A" && ip.To4() == nil {
		return fmt.Errorf("failover IP %s is not a valid IPv4 address for A record", ipAddress)
	} else if recordType == "AAAA" && ip.To4() != nil {
		return fmt.Errorf("failover IP %s is not a valid IPv6 address for AAAA record", ipAddress)
	}

	return nil
}

// useAutoGeneratedIP はIPアドレスを自動生成して使用する
func (s *Service) useAutoGeneratedIP(ctx context.Context, origin config.OriginConfig, unhealthyRecord cf.DNSRecord, dnsClient cloudflare.DNSClientInterface) error {
	ip := net.ParseIP(unhealthyRecord.Content)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", unhealthyRecord.Content)
	}

	var newIP string
	switch origin.RecordType {
	case "A":
		// IPv4の場合は最後のオクテットを+1する
		ipv4 := ip.To4()
		if ipv4 == nil {
			return fmt.Errorf("invalid IPv4 address: %s", unhealthyRecord.Content)
		}
		ipv4[3]++
		newIP = ipv4.String()
	case "AAAA":
		// IPv6の場合は最後のセグメントを+1する
		ipv6 := ip.To16()
		if ipv6 == nil {
			return fmt.Errorf("invalid IPv6 address: %s", unhealthyRecord.Content)
		}
		ipv6[15]++
		newIP = ipv6.String()
	default:
		return fmt.Errorf("unsupported record type: %s", origin.RecordType)
	}

	// 既存のレコードを削除して新しいレコードを作成
	log.Printf("Replacing unhealthy record %s with auto-generated IP: %s (no failover IPs configured, proxied: %t)",
		unhealthyRecord.Content, newIP, origin.Proxied)
	return dnsClient.ReplaceRecords(ctx, origin.Name, origin.RecordType, newIP)
}
