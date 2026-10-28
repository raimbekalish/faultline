package main

import (
	"context"
	"fmt"
	"github.com/raimbekalish/faultline/internal/buildinfo"
	"io"
	"os"
	"os/signal"
	"syscall"
)

var revision = "development"

func execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		if err := buildinfo.Write(out); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return executeRun(ctx, args[1:], out, stderr)
		case "lab":
			return executeLab(ctx, args[1:], out, stderr)
		case "bundle":
			return executeBundle(ctx, args[1:], out, stderr)
		case "history":
			return executeHistory(ctx, args[1:], out, stderr)
		case "evidence":
			if len(args) == 1 {
				return evidence(ctx, out, stderr)
			}
		}
	}
	fmt.Fprintln(stderr, "usage: faultline {run|history|bundle|evidence}; run accepts -spec and optional -db")
	return 2
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
