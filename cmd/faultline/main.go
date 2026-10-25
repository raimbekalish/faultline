package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

var revision = "development"

func execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return executeRun(ctx, args[1:], out, stderr)
		case "history":
			return executeHistory(ctx, args[1:], out, stderr)
		case "evidence":
			if len(args) == 1 {
				return evidence(ctx, out, stderr)
			}
		}
	}
	fmt.Fprintln(stderr, "usage: faultline {run|history|evidence}; run accepts -spec and optional -db")
	return 2
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
