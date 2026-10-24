// Package localhttp supplies bounded clients for the isolated sample topology.
package localhttp

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const MaxBody = 64 << 10

// New ignores proxy environment variables and refuses redirects so a configured
// local endpoint cannot redirect traffic to an unrelated system.
func New(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil, MaxIdleConns: 4, MaxIdleConnsPerHost: 2,
			IdleConnTimeout: 10 * time.Second, ResponseHeaderTimeout: timeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func ReadBody(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBody {
		return nil, fmt.Errorf("response exceeds %d bytes", MaxBody)
	}
	return b, nil
}
