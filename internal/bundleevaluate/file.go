package bundleevaluate

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ReadFile opens only an explicitly selected, bounded regular policy file.
// Policies have no includes, targets, environment interpolation or output paths.
func ReadFile(ctx context.Context, path string) (Policy, error) {
	if err := ctx.Err(); err != nil {
		return Policy{}, err
	}
	if path == "" || strings.Contains(path, "://") {
		return Policy{}, fmt.Errorf("policy must be a local regular file")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Policy{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxPolicyBytes {
		return Policy{}, fmt.Errorf("policy must be a bounded regular file, not a symlink")
	}
	f, err := openPolicy(path)
	if err != nil {
		return Policy{}, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil {
		return Policy{}, err
	}
	if !current.Mode().IsRegular() || current.Size() > MaxPolicyBytes || !os.SameFile(info, current) {
		return Policy{}, fmt.Errorf("policy input changed while opening")
	}
	return Decode(ctx, f)
}
