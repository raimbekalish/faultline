package lab

import "errors"

var ErrBudget = errors.New("journal cleanup reserve exhausted; cleanup may proceed only with unconfirmed recording")

type EventBudget struct {
	Remaining        int
	MandatoryCleanup int
	RetryReserve     int
}

// Cleanup cost is a conservative full attempt, ending in complete OR explicit
// unconfirmed state. It is not a promise that unknown resources can be removed.
func cleanupCost(s JournalState) int {
	if s.LabCleanup == "complete" || s.LabCleanup == "unconfirmed" {
		return 0
	}
	n := 1 // final complete/unconfirmed event
	if s.LabCleanup == "not_started" {
		n++
	}
	if s.Worker == "intent" || s.Worker == "running" {
		n++
	}
	switch s.ProxyCleanup {
	case "unconfirmed":
		n += 2
	case "intent":
		n++
	}
	for _, r := range s.Resources {
		switch r.State {
		case "create_intent":
			n++
		case "observed", "delete_unknown":
			n += 2
		case "delete_intent":
			n++
		}
	}
	return n
}
func (j Journal) Budget() (EventBudget, error) {
	s, e := j.State()
	if e != nil {
		return EventBudget{}, e
	}
	// A second full attempt: eight intent/result pairs, proxy pair, lab
	// begin/end and one worker closure: at most 21 additional events.
	retry := 0
	if s.LabCleanup != "complete" {
		retry = 21
	}
	return EventBudget{MaxJournalEvents - len(j.Events), cleanupCost(s), retry}, nil
}
func cleanupOperation(op Operation) bool {
	switch op {
	case Abort, CreateAbsent, CreateUnknown, WorkerExited, WorkerUnknown, ProxyCleanupIntent, ProxyAbsent, ProxyUnconfirmed, CleanupBegin, DeleteIntent, DeleteAbsent, DeleteUnknown, CleanupUnconfirmed, CleanupComplete:
		return true
	}
	return false
}
func admitEvent(next Journal, e Event) error {
	b, err := next.Budget()
	if err != nil {
		return err
	}
	reserve := b.MandatoryCleanup
	if !cleanupOperation(e.Operation) {
		reserve += b.RetryReserve
	}
	if b.Remaining < reserve {
		return ErrBudget
	}
	return nil
}
