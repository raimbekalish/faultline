package lab

import (
	"errors"
	"testing"
)

func TestCleanupBudgetReserveAndLegacyRead(t *testing.T) {
	j, a := consumed(t)
	j = createAll(t, j, a)
	j = advance(t, j, a, Event{Operation: WorkerIntent})
	j = advance(t, j, a, Event{Operation: WorkerRunning})
	for {
		n, e := j.Advance(a, Event{Operation: ProxyCleanupIntent})
		if e != nil {
			if !errors.Is(e, ErrBudget) {
				t.Fatal(e)
			}
			break
		}
		j = n
		n, e = j.Advance(a, Event{Operation: ProxyUnconfirmed, Reason: "transport_failure"})
		if e != nil {
			if !errors.Is(e, ErrBudget) {
				t.Fatal(e)
			}
			break
		}
		j = n
	}
	b, _ := j.Budget()
	if b.Remaining < b.MandatoryCleanup {
		t.Fatal("lost reserve", b)
	}
	s, _ := j.State()
	if s.ProxyCleanup == "intent" {
		j = advance(t, j, a, Event{Operation: ProxyAbsent})
	}
	s, _ = j.State()
	if s.Worker == "running" {
		j = advance(t, j, a, Event{Operation: WorkerExited})
	}
	j = cleanupKnown(t, j, a)
	j = advance(t, j, a, Event{Operation: CleanupComplete})
	if _, e := j.Receipt(a); e != nil {
		t.Fatal(e)
	}
	// Old schema-1 journals remain readable even if they predate admission policy.
	legacy, a := consumed(t)
	legacy = createAll(t, legacy, a)
	legacy = advance(t, legacy, a, Event{Operation: WorkerIntent})
	legacy = advance(t, legacy, a, Event{Operation: WorkerRunning})
	for len(legacy.Events) < MaxJournalEvents-1 {
		legacy.Events = append(legacy.Events, Event{Operation: ProxyCleanupIntent}, Event{Operation: ProxyUnconfirmed, Reason: "transport_failure"})
	}
	if len(legacy.Events) > MaxJournalEvents {
		legacy.Events = legacy.Events[:MaxJournalEvents]
	}
	if legacy.Validate() != nil {
		t.Fatal("legacy read compatibility")
	}
	if _, e := legacy.Advance(a, Event{Operation: CleanupBegin}); e == nil {
		t.Fatal("invented capacity")
	}
}
func TestCleanupBudgetAtEveryCreationStage(t *testing.T) {
	for stop := 0; stop <= 8; stop++ {
		j, a := consumed(t)
		j = advance(t, j, a, Event{Operation: Prepare})
		for _, k := range resourceKeys[:stop] {
			j = advance(t, j, a, Event{Operation: CreateIntent, Key: k})
			j = advance(t, j, a, Event{Operation: Created, Key: k, Proof: syntheticProof(t, j, k)})
		}
		b, e := j.Budget()
		if e != nil || b.MandatoryCleanup > 21 || b.Remaining < b.MandatoryCleanup+b.RetryReserve {
			t.Fatal(stop, b, e)
		}
		j = cleanupKnown(t, j, a)
		j = advance(t, j, a, Event{Operation: CleanupComplete})
	}
}
