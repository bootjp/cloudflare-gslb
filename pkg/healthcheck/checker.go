package healthcheck

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/cockroachdb/errors"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type Checker interface {
	Check(ip string) error
}

var (
	ErrUnknownHealthCheckType = errors.New("unknown health check type")
	ErrUnexpectedStatusCode   = errors.New("unexpected status code")
	ErrUnexpectedICMPType     = errors.New("unexpected ICMP message type")
)

func NewChecker(hc config.HealthCheck) (Checker, error) {
	switch hc.Type {
	case "http":
		return &HttpChecker{
			Endpoint: hc.Endpoint,
			Host:     hc.Host,
			Timeout:  time.Duration(hc.Timeout) * time.Second,
			Scheme:   "http",
			Headers:  hc.Headers,
		}, nil
	case "https":
		return &HttpChecker{
			Endpoint:           hc.Endpoint,
			Host:               hc.Host,
			Timeout:            time.Duration(hc.Timeout) * time.Second,
			Scheme:             "https",
			InsecureSkipVerify: hc.InsecureSkipVerify,
			Headers:            hc.Headers,
		}, nil
	case "icmp":
		return &IcmpChecker{
			Timeout: time.Duration(hc.Timeout) * time.Second,
		}, nil
	default:
		return nil, errors.WithStack(ErrUnknownHealthCheckType)
	}
}

type HttpChecker struct {
	Endpoint           string
	Host               string
	Timeout            time.Duration
	Scheme             string
	InsecureSkipVerify bool
	Headers            map[string]string
}

func (h *HttpChecker) Check(ip string) error {
	u := &url.URL{
		Scheme: h.Scheme,
		Host:   ip,
		Path:   h.Endpoint,
	}
	url := u.String()

	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	if h.Host != "" {
		req.Host = h.Host
	}

	for key, value := range h.Headers {
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	client := &http.Client{}

	// HTTPSの場合はTLS設定を追加
	if h.Scheme == "https" {
		// #nosec G402 - InsecureSkipVerifyはユーザー設定に基づいて必要に応じて有効化される
		// このオプションは自己署名証明書を使用する環境でのヘルスチェックを可能にするために提供されている
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: h.InsecureSkipVerify,
				ServerName:         h.Host, // proper SNI for certificate validation
			},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return errors.WithStack(ErrUnexpectedStatusCode)
	}

	return nil
}

type IcmpChecker struct {
	Timeout time.Duration
}

func (i *IcmpChecker) Check(ip string) error {
	var protocol int
	var network string

	if isIPv6(ip) {
		network = "ip6:ipv6-icmp"
		protocol = 58
	} else {
		network = "ip4:icmp"
		protocol = 1
	}

	conn, err := icmp.ListenPacket(network, "")
	if err != nil {
		return errors.WithStack(err)
	}
	defer conn.Close()

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

	_, cancel := context.WithTimeout(context.Background(), i.Timeout)
	defer cancel()

	if _, err := conn.WriteTo(binMsg, &net.UDPAddr{IP: net.ParseIP(ip)}); err != nil {
		return errors.WithStack(err)
	}

	reply := make([]byte, 1500)

	if err := conn.SetReadDeadline(time.Now().Add(i.Timeout)); err != nil {
		return errors.WithStack(err)
	}

	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		return errors.WithStack(err)
	}

	parsedMsg, err := icmp.ParseMessage(protocol, reply[:n])
	if err != nil {
		return errors.WithStack(err)
	}

	if parsedMsg.Type != getICMPEchoReplyType(protocol) {
		return errors.WithStack(ErrUnexpectedICMPType)
	}

	return nil
}

func isIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}

func getICMPType(protocol int) icmp.Type {
	if protocol == 58 {
		return ipv6.ICMPTypeEchoRequest
	}
	return ipv4.ICMPTypeEcho
}

func getICMPEchoReplyType(protocol int) icmp.Type {
	if protocol == 58 {
		return ipv6.ICMPTypeEchoReply
	}
	return ipv4.ICMPTypeEchoReply
}
