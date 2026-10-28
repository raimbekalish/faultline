package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/raimbekalish/faultline/internal/lab"
)

func TestLabOnlyPlanUsageAndRedaction(t *testing.T) {
	for _, args := range [][]string{nil, {"run"}, {"cleanup"}, {"plan"}, {"plan", "--application", "a"}, {"plan", "--application", "a", "--out", "b", "extra"}, {"plan", "--application", "a", "--application", "c", "--out", "b"}, {"plan", "--application=a", "--out=b", "--out=c"}, {"plan", "--target", "PRIVATE-SECRET"}} {
		var out, stderr bytes.Buffer
		called := false
		code := executeLabWith(context.Background(), args, &out, &stderr, func(context.Context, string, string) (lab.Summary, error) { called = true; return lab.Summary{}, nil })
		if code != 2 || called || out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "PRIVATE-SECRET") {
			t.Fatal(args, code, called, out.String(), stderr.String())
		}
	}
	for _, e := range []error{context.Canceled, context.DeadlineExceeded, lab.ErrDescriptor, lab.ErrFile, lab.ErrEndpoint, lab.ErrInspection, lab.ErrMetadata, errors.New("PRIVATE-SECRET /private/path")} {
		var out, stderr bytes.Buffer
		code := executeLabWith(context.Background(), []string{"plan", "--application", "a", "--out", "b"}, &out, &stderr, func(context.Context, string, string) (lab.Summary, error) { return lab.Summary{}, e })
		if code != 1 || out.Len() != 0 || strings.Contains(stderr.String(), "PRIVATE") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
	for _, args := range [][]string{{"plan", "--application", "a", "--out", "b"}, {"plan", "-out=b", "-application=a"}} {
		var out, stderr bytes.Buffer
		code := executeLabWith(context.Background(), args, &out, &stderr, func(_ context.Context, a, b string) (lab.Summary, error) {
			if a != "a" || b != "b" {
				t.Fatal(a, b)
			}
			return lab.Summary{Status: "PLANNED", RuntimeQualification: "NOT_PERFORMED"}, nil
		})
		if code != 0 || !strings.Contains(out.String(), `"status": "PLANNED"`) || stderr.Len() != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
	for _, out := range []io.Writer{labShortOutput{}, labFailedOutput{}} {
		var stderr bytes.Buffer
		code := executeLabWith(context.Background(), []string{"plan", "--application", "a", "--out", "b"}, out, &stderr, func(context.Context, string, string) (lab.Summary, error) { return lab.Summary{}, nil })
		if code != 1 || strings.Contains(stderr.String(), "PRIVATE") {
			t.Fatal(code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"lab", "run"}, &out, &stderr); code != 2 || out.Len() != 0 {
		t.Fatal("custom execution exposed", code)
	}
}

type labShortOutput struct{}

func (labShortOutput) Write(b []byte) (int, error) { return len(b) / 2, nil }

type labFailedOutput struct{}

func (labFailedOutput) Write([]byte) (int, error) { return 0, errors.New("PRIVATE-SECRET") }

func TestTopLevelUsageIncludesPlanOnlyLab(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := execute(context.Background(), nil, &out, &stderr); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "|lab}") {
		t.Fatal(code, out.String(), stderr.String())
	}
	for _, verb := range []string{"run", "cleanup"} {
		out.Reset()
		stderr.Reset()
		if execute(context.Background(), []string{"lab", verb}, &out, &stderr) != 2 || out.Len() != 0 {
			t.Fatal("execution enabled")
		}
	}
}
