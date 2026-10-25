// Package bundlecli is the SQLite-free offline command surface.
package bundlecli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raimbekalish/faultline/internal/bundle"
	"io"
)

func Execute(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) != 2 || (args[0] != "verify" && args[0] != "inspect") {
		fmt.Fprintln(stderr, "usage: faultline-bundle {verify|inspect} FILE.flb")
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
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, "write inspection:", err)
		return 1
	}
	return 0
}
