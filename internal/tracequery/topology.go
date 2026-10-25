package tracequery

import "regexp"

var spanIDPattern = regexp.MustCompile(`^[a-f0-9]{16}$`)

func validTraceID(id string) bool {
	return traceIDPattern.MatchString(id) && id != "00000000000000000000000000000000"
}

func validSpanID(id string) bool {
	return spanIDPattern.MatchString(id) && id != "0000000000000000"
}

// completeSampleTopology validates one observed snapshot, without deduplicating,
// joining snapshots, inferring parents, or interpreting HTTP success as completeness.
// Correlation with an experiment observation is checked separately by the caller.
func completeSampleTopology(r Record) bool {
	if len(r.Spans) != 4 || !validTraceID(r.TraceID) {
		return false
	}
	expected := [...]struct{ service, kind, operation string }{
		{"faultline-runner", "client", "GET /work"},
		{"faultline-frontend", "server", "GET /work"},
		{"faultline-frontend", "client", "GET /value"},
		{"faultline-dependency", "server", "GET /value"},
	}
	var chain [4]*Span
	ids := make(map[string]bool, len(r.Spans))
	roots := 0
	for i := range r.Spans {
		s := &r.Spans[i]
		if s.TraceID != r.TraceID || !validSpanID(s.SpanID) || ids[s.SpanID] {
			return false
		}
		ids[s.SpanID] = true
		if s.ParentSpanID == "" {
			roots++
		} else if !validSpanID(s.ParentSpanID) {
			return false
		}
		matched := false
		for role, want := range expected {
			if s.Service == want.service && s.Kind == want.kind && s.Operation == want.operation {
				if chain[role] != nil {
					return false
				}
				chain[role] = s
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if roots != 1 {
		return false
	}
	for role, s := range chain {
		if s == nil {
			return false
		}
		if role == 0 {
			if s.ParentSpanID != "" {
				return false
			}
		} else if s.ParentSpanID != chain[role-1].SpanID {
			return false
		}
	}
	return true
}
