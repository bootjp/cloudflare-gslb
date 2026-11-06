package healthcheck

import (
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"sync"
	"time"
)

// probeTrace records only transport timings. It never records request headers.
type probeTrace struct {
	mu             sync.Mutex
	started        time.Time
	connectStarted time.Time
	connected      time.Time
	tlsStarted     time.Time
	tlsDone        time.Time
	firstByte      time.Time
	connectFailed  bool
	tlsFailed      bool
}

func newProbeTrace() *probeTrace {
	return &probeTrace{started: time.Now()}
}

func (p *probeTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			p.mu.Lock()
			p.connectStarted = time.Now()
			p.mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			p.mu.Lock()
			p.connected = time.Now()
			p.connectFailed = err != nil
			p.mu.Unlock()
		},
		TLSHandshakeStart: func() {
			p.mu.Lock()
			p.tlsStarted = time.Now()
			p.mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			p.mu.Lock()
			p.tlsDone = time.Now()
			p.tlsFailed = err != nil
			p.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			p.mu.Lock()
			p.firstByte = time.Now()
			p.mu.Unlock()
		},
	}
}

func (p *probeTrace) summary(scheme, resultPhase string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	phase := "request_setup"
	switch {
	case p.connectStarted.IsZero():
	case p.connectFailed || p.connected.IsZero():
		phase = "connect"
	case scheme == "https" && (p.tlsFailed || p.tlsDone.IsZero()):
		phase = "tls_handshake"
	case p.firstByte.IsZero():
		phase = "response_headers"
	default:
		phase = "response_body"
	}
	if resultPhase != "" {
		phase = resultPhase
	}

	return fmt.Sprintf("phase=%s elapsed_ms=%d connect_ms=%d tls_ms=%d first_byte_ms=%d",
		phase,
		time.Since(p.started).Milliseconds(),
		elapsedMillis(p.connectStarted, p.connected),
		elapsedMillis(p.tlsStarted, p.tlsDone),
		elapsedMillis(p.started, p.firstByte),
	)
}

func elapsedMillis(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() {
		return -1
	}
	return end.Sub(start).Milliseconds()
}
