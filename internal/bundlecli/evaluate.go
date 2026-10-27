package bundlecli

import (
	"context"
	"fmt"
	"io"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundleevaluate"
)

// Only evaluate uses these codes. Existing commands retain 0/1/2 semantics.
const (
	ExitPolicyFail    = 3
	ExitIndeterminate = 4
)

func evaluate(ctx context.Context, args []string, out, stderr io.Writer) int {
	// Keep a single, explicit spelling. In particular, do not reinterpret flags
	// as arbitrary positional paths or silently ignore additional arguments.
	if len(args) != 3 || args[0] == "" || args[1] != "--policy" || args[2] == "" {
		usage(stderr)
		return 2
	}
	evidence, err := bundle.ReadFile(ctx, args[0])
	if err != nil {
		fmt.Fprintln(stderr, "invalid evidence:", err)
		return 1
	}
	policy, err := bundleevaluate.ReadFile(ctx, args[2])
	if err != nil {
		fmt.Fprintln(stderr, "invalid policy:", err)
		return 1
	}
	result, err := bundleevaluate.Evaluate(ctx, evidence.Evidence, policy)
	if err != nil {
		fmt.Fprintln(stderr, "evaluate evidence:", err)
		return 1
	}
	if err = ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "evaluate evidence:", err)
		return 1
	}
	if code := encode(checkedWriter{out}, stderr, result); code != 0 {
		return code
	}
	switch result.Status {
	case bundleevaluate.Pass:
		return 0
	case bundleevaluate.Fail:
		return ExitPolicyFail
	case bundleevaluate.Indeterminate:
		return ExitIndeterminate
	default:
		fmt.Fprintln(stderr, "unsupported evaluation status")
		return 1
	}
}

type checkedWriter struct{ io.Writer }

func (w checkedWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}
