// Package toxiproxy controls only Faultline's isolated sample proxy server.
package toxiproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
)

const Endpoint = "http://toxiproxy:8474"
const Listen = "0.0.0.0:8666"
const Upstream = "dependency:8080"
const ToxicName = "faultline-latency"

var ownedName = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)

type Client struct {
	base string
	http *http.Client
}

func New() *Client { return &Client{base: Endpoint, http: localhttp.New(2 * time.Second)} }

func (c *Client) Close() { c.http.CloseIdleConnections() }

func proxyPath(name string) (string, error) {
	if !ownedName.MatchString(name) {
		return "", fmt.Errorf("refusing proxy name outside Faultline ownership namespace")
	}
	return "/proxies/" + url.PathEscape(name), nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, missingOK bool) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("toxiproxy %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := localhttp.ReadBody(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound && missingOK {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("toxiproxy %s %s: HTTP %d: %s", method, path, resp.StatusCode, b)
	}
	return nil
}

func (c *Client) Create(ctx context.Context, name string) error {
	if _, err := proxyPath(name); err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, "/proxies", struct {
		Name     string `json:"name"`
		Listen   string `json:"listen"`
		Upstream string `json:"upstream"`
		Enabled  bool   `json:"enabled"`
	}{name, Listen, Upstream, true}, false)
}

func (c *Client) AddLatency(ctx context.Context, name string, latency time.Duration) error {
	p, err := proxyPath(name)
	if err != nil {
		return err
	}
	if latency < time.Millisecond || latency > 5*time.Second || latency%time.Millisecond != 0 {
		return fmt.Errorf("latency must be whole milliseconds between 1ms and 5s")
	}
	return c.request(ctx, http.MethodPost, p+"/toxics", struct {
		Name       string           `json:"name"`
		Type       string           `json:"type"`
		Stream     string           `json:"stream"`
		Toxicity   float64          `json:"toxicity"`
		Attributes map[string]int64 `json:"attributes"`
	}{ToxicName, "latency", "downstream", 1, map[string]int64{"latency": latency.Milliseconds(), "jitter": 0}}, false)
}

func (c *Client) RemoveLatency(ctx context.Context, name string) error {
	p, err := proxyPath(name)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, p+"/toxics/"+ToxicName, nil, true)
}

func (c *Client) Delete(ctx context.Context, name string) error {
	p, err := proxyPath(name)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, p, nil, true)
}
