package history

import (
	"fmt"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/record"
	"github.com/raimbekalish/faultline/internal/spec"
)

func validateReport(r experiment.Report, sp spec.Spec) error {
	if err := record.Validate(r, sp); err != nil {
		return fmt.Errorf("%w: invalid experiment report", ErrCorrupt)
	}
	return nil
}
