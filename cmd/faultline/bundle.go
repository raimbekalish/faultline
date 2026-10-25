package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecli"
	"github.com/raimbekalish/faultline/internal/history"
	"io"
)

func executeBundle(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: faultline bundle {export|verify|inspect}")
		return 2
	}
	if args[0] != "export" {
		return bundlecli.Execute(ctx, args, out, stderr)
	}
	fs := flag.NewFlagSet("bundle export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("db", defaultDB, "existing history database")
	dest := fs.String("out", "", "new .flb file; existing destinations refused")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *dest == "" || !history.ValidID(fs.Arg(0)) {
		fmt.Fprintln(stderr, "usage: faultline bundle export -db PATH -out NEW.flb RUN_ID")
		return 2
	}
	query, cancel := context.WithTimeout(ctx, history.OperationTimeout)
	s, err := history.OpenExisting(query, *path)
	if err != nil {
		cancel()
		fmt.Fprintln(stderr, err)
		return 1
	}
	e, err := s.Get(query, fs.Arg(0))
	closeErr := s.Close()
	cancel()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	evidence, err := history.Evidence(e)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	b, err := bundle.Build(ctx, evidence)
	if err == nil {
		err = bundle.Save(ctx, *dest, b)
	}
	if err != nil {
		fmt.Fprintln(stderr, "export evidence:", err)
		return 1
	}
	if _, err = fmt.Fprintf(out, "Exported validated evidence to %s (%d bytes); checksums are not authenticity.\n", *dest, len(b)); err != nil {
		fmt.Fprintln(stderr, "bundle published; output failed:", err)
		return 1
	}
	return 0
}
