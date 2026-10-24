package experiment

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/raimbekalish/faultline/internal/telemetry"
)

type Sample struct {
	DurationMS float64 `json:"duration_ms"`
	Status     int     `json:"status"`
	Error      string  `json:"error,omitempty"`
	TraceID    string  `json:"trace_id,omitempty"`
	SpanID     string  `json:"span_id,omitempty"`
}

type Summary struct {
	Requests    int     `json:"requests"`
	Failures    int     `json:"failures"`
	FailureRate float64 `json:"failure_rate"`
	MinMS       float64 `json:"min_ms"`
	MeanMS      float64 `json:"mean_ms"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	P99MS       float64 `json:"p99_ms"`
	MaxMS       float64 `json:"max_ms"`
}

type Phase struct {
	Samples []Sample `json:"samples"`
	Summary Summary  `json:"summary"`
}

func summarize(samples []Sample) Summary {
	s := Summary{Requests: len(samples)}
	if len(samples) == 0 {
		return s
	}
	durations := make([]float64, len(samples))
	for i, sample := range samples {
		durations[i] = sample.DurationMS
		s.MeanMS += sample.DurationMS
		if sample.Error != "" || sample.Status < 200 || sample.Status >= 300 {
			s.Failures++
		}
	}
	sort.Float64s(durations)
	s.MeanMS /= float64(s.Requests)
	s.FailureRate = float64(s.Failures) / float64(s.Requests)
	s.MinMS, s.MaxMS = durations[0], durations[len(durations)-1]
	percentile := func(p float64) float64 { return durations[int(math.Ceil(p*float64(len(durations))))-1] }
	s.P50MS, s.P95MS, s.P99MS = percentile(.5), percentile(.95), percentile(.99)
	return s
}

// measure includes failed-request duration in the distribution; transport and
// non-2xx outcomes are measurements, while parent cancellation aborts the phase.
type observation struct {
	tracing *telemetry.Runtime
	runID   string
	phase   string
}

func measure(ctx context.Context, client *http.Client, target string, count int, obs observation) (phase Phase, err error) {
	defer func() { phase.Summary = summarize(phase.Samples) }()
	for i := range count {
		if err := ctx.Err(); err != nil {
			return phase, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return phase, err
		}
		start := time.Now()
		result := obs.tracing.Do(client, req, "/work", telemetry.Metadata{RunID: obs.runID, Phase: obs.phase, Observation: i})
		requestErr := result.Err
		sample := Sample{Status: result.Status, TraceID: result.TraceID, SpanID: result.SpanID}
		sample.DurationMS = float64(time.Since(start)) / float64(time.Millisecond)
		if requestErr != nil {
			sample.Error = requestErr.Error()
		} else if sample.Status < 200 || sample.Status >= 300 {
			sample.Error = fmt.Sprintf("HTTP %d", sample.Status)
		}
		phase.Samples = append(phase.Samples, sample)
		if ctx.Err() != nil {
			return phase, ctx.Err()
		}
	}
	return phase, nil
}
