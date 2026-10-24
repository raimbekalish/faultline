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
	TraceID string `json:"traceID"`
	Spans   []struct {
		TraceID    string `json:"traceID"`
		SpanID     string `json:"spanID"`
		Operation  string `json:"operationName"`
		ProcessID  string `json:"processID"`
		Duration   int64  `json:"duration"`
		References []struct {
			Type    string `json:"refType"`
			TraceID string `json:"traceID"`
			SpanID  string `json:"spanID"`
		} `json:"references"`
		Tags []struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		} `json:"tags"`
	} `json:"spans"`
	Processes map[string]struct {
		Service string `json:"serviceName"`
	} `json:"processes"`
}

func decode(id string, body []byte) (Record, error) {
	r := Record{TraceID: id}
	var response struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return r, err
	}
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
			for _, ref := range s.References {
				if ref.Type == "CHILD_OF" && ref.TraceID == id {
					span.ParentSpanID = ref.SpanID
					break
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
	// Four is the expected sample topology, not proof of semantic completeness;
	// tests additionally verify services, metadata, IDs and every parent edge.
	r.Complete = len(r.Spans) == 4
	return r, nil
}

func (c *Client) fetch(ctx context.Context, id string) (Record, error) {
	r := Record{TraceID: id}
	if !traceIDPattern.MatchString(id) {
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
		if !traceIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid trace ID")
		}
		records[i].TraceID = id
	}
	for {
		complete := true
		for i, id := range ids {
			if records[i].Complete {
				continue
			}
			r, err := c.fetch(ctx, id)
			if len(r.Spans) >= len(records[i].Spans) {
				records[i] = r
			}
			if err != nil {
				records[i].RetrievalError = "Jaeger query failed"
			}
			if !records[i].Complete {
				complete = false
			}
			if ctx.Err() != nil {
				break
			}
		}
		complete = true
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
