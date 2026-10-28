package labworker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
)

const contractURL = "http://frontend:8080/work"
const maxBody = 64 << 10

// contractClient has no public endpoint constructor. Only same-package scoped
// tests construct it. No production CLI/worker imports or activates this engine.
// A future real-runtime binding requires a separate reviewed implementation.
type contractClient struct {
	client    *http.Client
	transport *http.Transport
}

func newHarnessClient(address netip.AddrPort, timeout time.Duration) (*contractClient, error) {
	if !address.IsValid() || !address.Addr().IsLoopback() || address.Port() == 0 || timeout < 10*time.Millisecond || timeout > 5*time.Second {
		return nil, ErrConfiguration
	}
	dialer := net.Dialer{Timeout: timeout, KeepAlive: -1}
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1,
		MaxIdleConns: 1, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: timeout, MaxResponseHeaderBytes: 8192,
		DialContext: func(ctx context.Context, network, host string) (net.Conn, error) {
			if network != "tcp" || host != "frontend:8080" {
				return nil, ErrConfiguration
			}
			return dialer.DialContext(ctx, "tcp", address.String())
		},
	}
	return &contractClient{&http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport}, nil
}
func (c *contractClient) close() { c.transport.CloseIdleConnections() }
func (c *contractClient) observe(ctx context.Context) experiment.Sample {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, contractURL, nil)
	response, e := c.client.Do(req)
	sample := experiment.Sample{}
	if e != nil {
		sample.Error = classify(ctx, e)
	} else {
		sample.Status = response.StatusCode
		n, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxBody+1))
		_ = response.Body.Close()
		switch {
		case err != nil:
			sample.Error = classify(ctx, err)
			if sample.Error == "transport_error" {
				sample.Error = "body_read_error"
			}
		case n > maxBody:
			sample.Error = "body_too_large"
		case sample.Status < 200 || sample.Status >= 300:
			sample.Error = "http_status"
		}
	}
	sample.DurationMS = float64(time.Since(start)) / float64(time.Millisecond)
	return sample
}
func classify(ctx context.Context, e error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "request_canceled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "context_deadline"
	}
	var timeout net.Error
	if errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &timeout) && timeout.Timeout()) {
		return "request_timeout"
	}
	var network *net.OpError
	if errors.As(e, &network) {
		return "connection_error"
	}
	return "transport_error"
}
