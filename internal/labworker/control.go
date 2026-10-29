package labworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const controlHost = "toxiproxy-control:8474"
const toxicName = "faultline-latency"
const maxControlBody = 32 << 10
const maxControlRequests = 7

var ErrControl = errors.New("scoped fault control unconfirmed")

type latencyAttributes struct {
	Latency int `json:"latency"`
	Jitter  int `json:"jitter"`
}
type latencyToxic struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Stream     string            `json:"stream"`
	Toxicity   float64           `json:"toxicity"`
	Attributes latencyAttributes `json:"attributes"`
}
type proxyState struct {
	Name     string         `json:"name"`
	Listen   string         `json:"listen"`
	Upstream string         `json:"upstream"`
	Enabled  bool           `json:"enabled"`
	Toxics   []latencyToxic `json:"toxics"`
}
type controlOperation uint8

const (
	readScopedProxy controlOperation = iota
	addScopedToxic
	removeScopedToxic
)

// Constructors are private and loopback-only. This controller implements the
// approved HTTP contract but cannot target a production daemon or arbitrary URL.
// The test harness explicitly owns the server. API names are NOT authentication.
type scopedController struct {
	mu                                sync.Mutex
	expected                          LatencyIntent
	client                            *http.Client
	transport                         *http.Transport
	calls                             int
	attempted, acknowledged, removing bool
}

func (*scopedController) MarshalJSON() ([]byte, error) { return nil, ErrControl }
func (*scopedController) UnmarshalJSON([]byte) error   { return ErrControl }

func newScopedController(c Configuration, address netip.AddrPort) (*scopedController, error) {
	if c.validate() != nil || !address.IsValid() || !address.Addr().IsLoopback() || address.Port() == 0 {
		return nil, ErrConfiguration
	}
	dial := net.Dialer{Timeout: 2 * time.Second, KeepAlive: -1}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: 2 * time.Second, MaxResponseHeaderBytes: 8192,
		DialContext: func(ctx context.Context, network, host string) (net.Conn, error) {
			if network != "tcp" || host != controlHost {
				return nil, ErrControl
			}
			return dial.DialContext(ctx, "tcp", address.String())
		}}
	return &scopedController{expected: intent(c), client: &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport: transport}, nil
}
func (c *scopedController) close() { c.transport.CloseIdleConnections() }
func (c *scopedController) toxic() latencyToxic {
	return latencyToxic{toxicName, "latency", "downstream", 1, latencyAttributes{c.expected.LatencyMS, 0}}
}
func (c *scopedController) request(ctx context.Context, op controlOperation) ([]byte, error) {
	if ctx.Err() != nil || c.calls >= maxControlRequests {
		return nil, ErrControl
	}
	method, path, status := http.MethodGet, "/proxies/"+c.expected.RunID, 200
	var data []byte
	switch op {
	case readScopedProxy:
	case addScopedToxic:
		method = http.MethodPost
		path += "/toxics"
		data, _ = json.Marshal(c.toxic())
	case removeScopedToxic:
		method = http.MethodDelete
		path += "/toxics/" + toxicName
		status = 204
	default:
		return nil, ErrControl
	}
	req, _ := http.NewRequestWithContext(ctx, method, "http://"+controlHost+path, bytes.NewReader(data))
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.calls++
	resp, e := c.client.Do(req)
	if e != nil {
		return nil, ErrControl
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxControlBody+1))
	if e != nil || len(b) > maxControlBody || resp.StatusCode != status || ctx.Err() != nil {
		return nil, ErrControl
	}
	if status == 204 {
		if len(b) != 0 {
			return nil, ErrControl
		}
	} else {
		typ, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || typ != "application/json" {
			return nil, ErrControl
		}
	}
	return b, nil
}
func (c *scopedController) proxy(ctx context.Context) (proxyState, error) {
	b, e := c.request(ctx, readScopedProxy)
	var p proxyState
	if e != nil || decodeExact(b, &p) != nil || p.Name != c.expected.RunID || p.Listen != "0.0.0.0:8666" || p.Upstream != "dependency:8080" || !p.Enabled || p.Toxics == nil || len(p.Toxics) > 1 {
		return proxyState{}, ErrControl
	}
	if len(p.Toxics) == 1 && p.Toxics[0] != c.toxic() {
		return proxyState{}, ErrControl
	}
	return p, nil
}
func (c *scopedController) ApplyLatency(ctx context.Context, i LatencyIntent) (ApplicationState, error) {
	if !c.mu.TryLock() {
		return ApplicationUnknown, ErrControl
	}
	defer c.mu.Unlock()
	if i != c.expected || c.attempted || c.removing || ctx.Err() != nil {
		return ApplicationUnknown, ErrControl
	}
	p, e := c.proxy(ctx)
	if e != nil || len(p.Toxics) != 0 {
		return ApplicationUnknown, ErrControl
	}
	c.attempted = true // uncertain POST outcomes never regain apply authority
	b, e := c.request(ctx, addScopedToxic)
	var toxic latencyToxic
	if e != nil || decodeExact(b, &toxic) != nil || toxic != c.toxic() {
		return ApplicationUnknown, ErrControl
	}
	c.acknowledged = true
	p, e = c.proxy(ctx)
	if e != nil || len(p.Toxics) != 1 {
		return ApplicationUnknown, ErrControl
	}
	return Applied, nil
}
func (c *scopedController) RemoveLatency(ctx context.Context, i LatencyIntent) error {
	if !c.mu.TryLock() {
		return ErrControl
	}
	defer c.mu.Unlock()
	if i != c.expected || !c.acknowledged || c.removing || ctx.Err() != nil {
		return ErrControl
	}
	p, e := c.proxy(ctx)
	if e != nil || len(p.Toxics) != 1 {
		return ErrControl
	}
	c.removing = true
	_, e = c.request(ctx, removeScopedToxic)
	return e
}
func (c *scopedController) VerifyRemoved(ctx context.Context, i LatencyIntent) (Presence, error) {
	if !c.mu.TryLock() {
		return Unknown, ErrControl
	}
	defer c.mu.Unlock()
	if i != c.expected || !c.acknowledged || ctx.Err() != nil {
		return Unknown, ErrControl
	}
	a, e := c.proxy(ctx)
	if e != nil {
		return Unknown, ErrControl
	}
	b, e := c.proxy(ctx)
	if e != nil || len(a.Toxics) != len(b.Toxics) {
		return Unknown, ErrControl
	}
	if len(a.Toxics) == 0 {
		return Absent, nil
	}
	return Present, nil
}
