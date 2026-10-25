package experiment

import "testing"

func TestCompareSummariesPreservesUnitsAndDirection(t *testing.T) {
	a := Summary{MeanMS: 2, P95MS: 4, FailureRate: .25}
	b := Summary{MeanMS: 5, P95MS: 9, FailureRate: .75}
	if got := CompareSummaries(a, b); got != (Comparison{MeanDeltaMS: 3, P95DeltaMS: 5, FailureRateDelta: .5}) {
		t.Fatal(got)
	}
	if got := CompareSummaries(b, a); got != (Comparison{MeanDeltaMS: -3, P95DeltaMS: -5, FailureRateDelta: -.5}) {
		t.Fatal(got)
	}
	if got := CompareSummaries(a, a); got != (Comparison{}) {
		t.Fatal(got)
	}
}
