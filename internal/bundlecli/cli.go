// Package bundlecli is the SQLite-free offline command surface.
package bundlecli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raimbekalish/faultline/internal/buildinfo"
	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecompare"
	"io"
)

func Execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		if err := buildinfo.Write(out); err != nil {
			fmt.Fprintln(stderr, "write version:", err)
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "compare" {
		if len(args) != 3 {
			usage(stderr)
			return 2
		}
		left, err := bundle.ReadFile(ctx, args[1])
		if err != nil {
			fmt.Fprintln(stderr, "invalid left evidence:", err)
			return 1
		}
		right, err := bundle.ReadFile(ctx, args[2])
		if err != nil {
			fmt.Fprintln(stderr, "invalid right evidence:", err)
			return 1
		}
		result, err := bundlecompare.Compare(ctx, left.Evidence, right.Evidence)
		if err != nil {
			fmt.Fprintln(stderr, "compare evidence:", err)
			return 1
		}
		return encode(out, stderr, result)
	}
	if len(args) != 2 || (args[0] != "verify" && args[0] != "inspect") {
		usage(stderr)
		return 2
	}
	inspection, err := bundle.ReadFile(ctx, args[1])
	if err != nil {
		fmt.Fprintln(stderr, "invalid evidence:", err)
		return 1
	}
	var value any = inspection
	if args[0] == "verify" {
		value = struct {
			Version   int             `json:"format_version"`
			Integrity string          `json:"integrity"`
			History   bundle.Metadata `json:"history"`
			Warnings  []string        `json:"warnings"`
		}{inspection.Version, inspection.Integrity, inspection.History, inspection.Warnings}
	}
	return encode(out, stderr, value)
}
func usage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: faultline-bundle {verify|inspect} FILE.flb | compare LEFT.flb RIGHT.flb")
}
func encode(out, stderr io.Writer, value any) int {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, "write evidence result:", err)
		return 1
	}
	return 0
}
