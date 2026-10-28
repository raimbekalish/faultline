package main

import (
	"bytes"
	"context"
	"testing"
)

func TestWorkerCannotExecuteExperiments(t *testing.T) {
	for _, args := range [][]string{nil, {"run"}, {"lab-worker", "--execute"}, {"lab-worker", "--target", "http://example.invalid"}} {
		var out, err bytes.Buffer
		if execute(context.Background(), args, bytes.NewBufferString(`{}`), &out, &err) != 2 || out.Len() != 0 {
			t.Fatal(args)
		}
	}
	var out, err bytes.Buffer
	if execute(context.Background(), []string{"lab-worker"}, bytes.NewBufferString(`{}`), &out, &err) != 1 || out.Len() != 0 {
		t.Fatal("bad input accepted")
	}
}
