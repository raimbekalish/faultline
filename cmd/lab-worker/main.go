package main

import (
	"context"
	"fmt"
	"github.com/raimbekalish/faultline/internal/labprotocol"
	"io"
	"os"
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
func main() { os.Exit(execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
