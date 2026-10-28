package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/raimbekalish/faultline/internal/lab"
)

func executeLab(ctx context.Context, args []string, out, stderr io.Writer) int {
	return executeLabWith(ctx, args, out, stderr, lab.Create)
}

func executeLabWith(ctx context.Context, args []string, out, stderr io.Writer, plan func(context.Context, string, string) (lab.Summary, error)) int {
	usage := func() {
		fmt.Fprintln(stderr, "usage: faultline lab plan --application FILE.json --out PLAN.json (planning only; run/cleanup unavailable)")
	}
	if len(args) == 0 || args[0] != "plan" {
		usage()
		return 2
	}
	f := flag.NewFlagSet("lab plan", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var application, output onceFlag
	f.Var(&application, "application", "local descriptor")
	f.Var(&output, "out", "private no-replace plan")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || application.value == "" || output.value == "" {
		usage()
		return 2
	}
	summary, err := plan(ctx, application.value, output.value)
	if err != nil {
		fmt.Fprintln(stderr, "lab plan:", lab.PublicError(err), "; private output may already exist")
		return 1
	}
	if err = summary.Write(out); err != nil {
		fmt.Fprintln(stderr, "lab plan: cannot write summary; private plan may already exist")
		return 1
	}
	return 0
}

type onceFlag struct {
	value string
	set   bool
}

func (f *onceFlag) String() string { return f.value }
func (f *onceFlag) Set(s string) error {
	if f.set {
		return errors.New("duplicate option")
	}
	f.value, f.set = s, true
	return nil
}
