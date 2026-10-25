// faultline-bundle is a pure-Go offline verifier/inspector. It never opens history.
package main

import (
	"context"
	"github.com/raimbekalish/faultline/internal/bundlecli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := bundlecli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
