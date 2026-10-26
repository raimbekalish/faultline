package main

import (
	"bytes"
	"context"
	"testing"
)

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"run", "-samples", "101"}, {"run", "-latency", "1us"}, {"run", "-request-timeout", "0s"}, {"run", "extra"}, {"run", "-target", "http://example.com"}} {
		var out, stderr bytes.Buffer
		if code := execute(context.Background(), args, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%q", args, code, out.String())
		}
	}
}

func TestVersionNeedsNoHistory(t *testing.T) {
	var out, err bytes.Buffer
	if code := execute(context.Background(), []string{"--version"}, &out, &err); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"revision"`)) {
		t.Fatal(code, out.String(), err.String())
	}
}
