// Package tracequery reads the pinned local Jaeger JSON query API. This is a
// validation adapter, not a general-purpose trace backend or diagnostic engine.
package tracequery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/raimbekalish/faultline/internal/localhttp"
)

var traceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Span struct {
	TraceID      string  `json:"trace_id"`
	SpanID       string  `json:"span_id"`
	ParentSpanID string  `json:"parent_span_id,omitempty"`
	Service      string  `json:"service"`
	Operation    string  `json:"operation"`
	Kind         string  `json:"kind"`
	DurationMS   float64 `json:"duration_ms"`
	HTTPStatus   int     `json:"http_status,omitempty"`
	ErrorType    string  `json:"error_type,omitempty"`
	OTelStatus   string  `json:"otel_status,omitempty"`
	RunID        string  `json:"run_id,omitempty"`
	Phase        string  `json:"phase,omitempty"`
	Observation  string  `json:"observation,omitempty"`
}

type Record struct {
	TraceID        string          `json:"trace_id"`
	Complete       bool            `json:"complete"`
	Spans          []Span          `json:"spans"`
	RetrievalError string          `json:"retrieval_error,omitempty"`
	Jaeger         json.RawMessage `json:"jaeger,omitempty"`
}

type Client struct {
	base string
	http *http.Client
}

func New() *Client       { return &Client{base: "http://jaeger:16686", http: localhttp.New(time.Second)} }
func (c *Client) Close() { c.http.CloseIdleConnections() }

type rawTrace struct {
	TraceID   string                `json:"traceID"`
	Spans     []rawSpan             `json:"spans"`
	Processes map[string]rawProcess `json:"processes"`
}

type rawProcess struct {
	Service string `json:"serviceName"`
}

type rawReference struct {
	Type    string `json:"refType"`
	TraceID string `json:"traceID"`
	SpanID  string `json:"spanID"`
}

type rawTag struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type rawSpan struct {
	TraceID    string         `json:"traceID"`
	SpanID     string         `json:"spanID"`
	Operation  string         `json:"operationName"`
	ProcessID  string         `json:"processID"`
	Duration   int64          `json:"duration"`
	References []rawReference `json:"references"`
	Tags       []rawTag       `json:"tags"`
}

func decode(id string, body []byte) (Record, error) {
	r := Record{TraceID: id}
	if !validTraceID(id) {
		return r, fmt.Errorf("invalid trace ID")
	}
	var response struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return r, err
	}
	// The trace-by-ID endpoint must return at most one trace object. Combining
	// separate objects could fabricate a complete graph from partial snapshots.
	if len(response.Data) > 1 {
		return r, fmt.Errorf("query returned multiple trace objects")
	}
	referencesValid := true
	for _, data := range response.Data {
		var raw rawTrace
		if err := json.Unmarshal(data, &raw); err != nil {
			return r, err
		}
		if raw.TraceID != id {
			return r, fmt.Errorf("query returned another trace")
		}
		r.Jaeger = data
		for _, s := range raw.Spans {
			if s.TraceID != id {
				return r, fmt.Errorf("span belongs to another trace")
			}
			span := Span{TraceID: id, SpanID: s.SpanID, Service: raw.Processes[s.ProcessID].Service, Operation: s.Operation, DurationMS: float64(s.Duration) / 1000}
			// The fixed topology permits no links and at most one same-trace
			// CHILD_OF reference. Never silently discard an ambiguous parent.
			if len(s.References) > 1 {
				referencesValid = false
			}
			if len(s.References) == 1 {
				ref := s.References[0]
				if ref.Type == "CHILD_OF" && ref.TraceID == id && validSpanID(ref.SpanID) {
					span.ParentSpanID = ref.SpanID
				} else {
					referencesValid = false
				}
			}
			for _, tag := range s.Tags {
				v := fmt.Sprint(tag.Value)
				switch tag.Key {
				case "span.kind":
					span.Kind = v
				case "http.response.status_code", "http.status_code":
					span.HTTPStatus, _ = strconv.Atoi(v)
				case "error.type":
					span.ErrorType = v
				case "otel.status_code":
					span.OTelStatus = v
				case "faultline.run_id":
					span.RunID = v
				case "faultline.phase":
					span.Phase = v
				case "faultline.observation":
					span.Observation = v
				}
			}
			r.Spans = append(r.Spans, span)
		}
	}
	r.Complete = referencesValid && completeSampleTopology(r)
	return r, nil
}

func (c *Client) fetch(ctx context.Context, id string) (Record, error) {
	r := Record{TraceID: id}
	if !validTraceID(id) {
		return r, fmt.Errorf("invalid trace ID")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/api/traces/"+id, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return r, nil
	}
	if resp.StatusCode != 200 {
		return r, fmt.Errorf("query HTTP %d", resp.StatusCode)
	}
	body, err := localhttp.ReadBody(resp.Body)
	if err != nil {
		return r, err
	}
	return decode(id, body)
}

// Collect polls a bounded set of observations under one shared caller deadline.
// Partial/missing traces remain explicit; no causal explanation is invented.
func (c *Client) Collect(ctx context.Context, ids []string) ([]Record, error) {
	if len(ids) > 300 {
		return nil, fmt.Errorf("at most 300 trace IDs per collection")
	}
	records := make([]Record, len(ids))
	for i, id := range ids {
		if !validTraceID(id) {
			return nil, fmt.Errorf("invalid trace ID")
		}
		records[i].TraceID = id
	}
	for {
		for i, id := range ids {
			if ctx.Err() != nil {
				break
			}
			if records[i].Complete {
				continue
			}
			r, err := c.fetch(ctx, id)
			// A corrected complete snapshot supersedes even a larger malformed
			// response. Keep one observed snapshot; never merge spans across polls.
			if r.Complete || len(r.Spans) >= len(records[i].Spans) {
				records[i] = r
			}
			if err != nil {
				records[i].RetrievalError = "Jaeger query failed"
			}
			if ctx.Err() != nil {
				break
			}
		}
		complete := true
		for _, r := range records {
			if !r.Complete {
				complete = false
			}
		}
		if complete {
			return records, nil
		}
		select {
		case <-ctx.Done():
			for i := range records {
				if !records[i].Complete && records[i].RetrievalError == "" {
					records[i].RetrievalError = "incomplete or missing at retrieval deadline"
				}
			}
			return records, nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}
