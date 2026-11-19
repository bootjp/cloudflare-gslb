package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"
)

const schemeHTTP = "http"

// URLChecker preserves the probe hostname as Host and TLS SNI. A redirect is not readiness.
type URLChecker struct {
	Timeout time.Duration
	Headers map[string]string
	// Host is only used for local replica probes against a virtual host on loopback.
	Host string
}

func (h *URLChecker) Check(rawURL string) error {
	return h.CheckContext(context.Background(), rawURL)
}

func (h *URLChecker) CheckContext(parent context.Context, rawURL string) error {
	u, err := readinessURL(rawURL)
	if err != nil {
		return err
	}
	if h.Timeout <= 0 {
		return fmt.Errorf("readiness timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(parent, h.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if h.Host != "" {
		req.Host = h.Host
	}
	for key, value := range h.Headers {
		req.Header.Set(key, value)
	}
	// Avoid reusing stale cache responses for fixed published probe names.
	req.Header.Set("Cache-Control", "no-cache")
	trace := newProbeTrace()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace.clientTrace()))
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: h.Timeout, ResponseHeaderTimeout: h.Timeout}
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", trace.summary(u.Scheme, ""), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d (%s)", ErrUnexpectedStatusCode, resp.StatusCode, trace.summary(u.Scheme, "http_status"))
	}
	return nil
}

func readinessURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP) || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid readiness URL")
	}
	return u, nil
}
