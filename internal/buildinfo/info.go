// Package buildinfo describes the executable, never an experiment's environment.
package buildinfo

import (
	"encoding/json"
	"io"
	"runtime"
	"runtime/debug"
)

// Set only by explicit linker flags in candidate builds. No wall-clock build date.
var Version = "development"
var Revision = "development"

type Info struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CGO       string `json:"cgo_enabled"`
}

func Current() Info {
	i := Info{Version, Revision, runtime.Version(), runtime.GOOS, runtime.GOARCH, "unknown"}
	if b, ok := debug.ReadBuildInfo(); ok {
		for _, s := range b.Settings {
			if s.Key == "CGO_ENABLED" {
				i.CGO = s.Value
			}
		}
	}
	return i
}
func Write(w io.Writer) error { return json.NewEncoder(w).Encode(Current()) }
