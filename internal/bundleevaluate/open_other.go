//go:build !darwin && !linux

package bundleevaluate

import "os"

// Other targets retain regular-file/identity checks, but are not runtime-qualified.
func openPolicy(path string) (*os.File, error) { return os.Open(path) }
