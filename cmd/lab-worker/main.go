package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raimbekalish/faultline/internal/labprotocol"
)

var revision = "development"

func execute(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "lab-worker" {
		fmt.Fprintln(stderr, "usage: catalog-worker lab-worker (preflight only; execution unavailable)")
		return 2
	}
	if labprotocol.Preflight(ctx, in, out, revision) != nil {
		fmt.Fprintln(stderr, "worker preflight rejected; no execution performed")
		return 1
	}
	return 0
}
func main() {
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	// A context check cannot interrupt a blocked pipe read. Close our own stdin
	// on cancellation; no arbitrary file or subprocess is accepted here.
	go func() { <-ctx.Done(); _ = os.Stdin.Close() }()
	os.Exit(execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
