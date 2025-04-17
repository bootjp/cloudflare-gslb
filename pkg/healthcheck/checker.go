package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/cockroachdb/errors"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Checker はヘルスチェックを行うインターフェース
type Checker interface {
	Check(ip string) error
}

// エラー定義
var (
	ErrUnknownHealthCheckType = errors.New("unknown health check type")
	ErrUnexpectedStatusCode   = errors.New("unexpected status code")
	ErrUnexpectedICMPType     = errors.New("unexpected ICMP message type")
)

// NewChecker は設定に基づいたヘルスチェッカーを作成する
func NewChecker(hc config.HealthCheck) (Checker, error) {
	switch hc.Type {
	case "http":
		return &HttpChecker{
			Endpoint: hc.Endpoint,
			Host:     hc.Host,
			Timeout:  time.Duration(hc.Timeout) * time.Second,
			Scheme:   "http",
		}, nil
	case "https":
		return &HttpChecker{
			Endpoint:           hc.Endpoint,
			Host:               hc.Host,
			Timeout:            time.Duration(hc.Timeout) * time.Second,
			Scheme:             "https",
			InsecureSkipVerify: hc.InsecureSkipVerify,
		}, nil
	case "icmp":
		return &IcmpChecker{
			Timeout: time.Duration(hc.Timeout) * time.Second,
		}, nil
	default:
		return nil, errors.WithStack(ErrUnknownHealthCheckType)
	}
}

// HttpChecker はHTTP/HTTPSでヘルスチェックを行う
type HttpChecker struct {
	Endpoint           string
	Host               string
	Timeout            time.Duration
	Scheme             string
	InsecureSkipVerify bool
}

// Check はHTTP/HTTPSでヘルスチェックを行う
func (h *HttpChecker) Check(ip string) error {
	transport := &http.Transport{}

	// HTTPSの場合、証明書検証の設定を適用
	if h.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{
			// #nosec G402 - InsecureSkipVerifyはユーザーが明示的に設定する場合のみ有効
			InsecureSkipVerify: h.InsecureSkipVerify,
		}
	}

	client := &http.Client{
		Timeout:   h.Timeout,
		Transport: transport,
	}

	url := fmt.Sprintf("%s://%s%s", h.Scheme, ip, h.Endpoint)
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	// ホスト名が指定されている場合はヘッダーを設定
	if h.Host != "" {
		req.Host = h.Host
		req.Header.Set("Host", h.Host)
	}

	resp, err := client.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	// 200番台のステータスコードであれば正常とみなす
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.WithStack(ErrUnexpectedStatusCode)
	}

	return nil
}

// IcmpChecker はICMPでヘルスチェックを行う
type IcmpChecker struct {
	Timeout time.Duration
}

// Check はICMPでヘルスチェックを行う
func (i *IcmpChecker) Check(ip string) error {
	var protocol int
	var network string

	// IPv4かIPv6かを判断
	if isIPv6(ip) {
		network = "ip6:ipv6-icmp"
		protocol = 58 // IPv6-ICMP
	} else {
		network = "ip4:icmp"
		protocol = 1 // ICMP
	}

	conn, err := icmp.ListenPacket(network, "")
	if err != nil {
		return errors.WithStack(err)
	}
	defer conn.Close()

	// ICMPエコーリクエストの作成
	msg := icmp.Message{
		Type: getICMPType(protocol),
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("PING"),
		},
	}

	binMsg, err := msg.Marshal(nil)
	if err != nil {
		return errors.WithStack(err)
	}

	// タイムアウトの設定
	// contextは直接使用していませんが、将来的な拡張性のために残しておきます
	_, cancel := context.WithTimeout(context.Background(), i.Timeout)
	defer cancel()

	// ICMPパケットの送信
	if _, err := conn.WriteTo(binMsg, &net.UDPAddr{IP: net.ParseIP(ip)}); err != nil {
		return errors.WithStack(err)
	}

	// 応答待機のためのバッファ
	reply := make([]byte, 1500)

	// 読み取りタイムアウトの設定
	if err := conn.SetReadDeadline(time.Now().Add(i.Timeout)); err != nil {
		return errors.WithStack(err)
	}

	// 応答の待機
	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		return errors.WithStack(err)
	}

	// 応答の解析
	parsedMsg, err := icmp.ParseMessage(protocol, reply[:n])
	if err != nil {
		return errors.WithStack(err)
	}

	// エコー応答の確認
	if parsedMsg.Type != getICMPEchoReplyType(protocol) {
		return errors.WithStack(ErrUnexpectedICMPType)
	}

	return nil
}

// isIPv6 はIPアドレスがIPv6かどうかを判断する
func isIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}

// getICMPType はプロトコルに基づいてICMPタイプを返す
func getICMPType(protocol int) icmp.Type {
	if protocol == 58 { // IPv6-ICMP
		return ipv6.ICMPTypeEchoRequest
	}
	return ipv4.ICMPTypeEcho
}

// getICMPEchoReplyType はプロトコルに基づいてICMPエコー応答タイプを返す
func getICMPEchoReplyType(protocol int) icmp.Type {
	if protocol == 58 { // IPv6-ICMP
		return ipv6.ICMPTypeEchoReply
	}
	return ipv4.ICMPTypeEchoReply
}
