package experiment

// CompareSummaries preserves the report/history right-minus-left arithmetic.
// FailureRateDelta is a fraction; consumers may label it in percentage points.
// Callers validate summaries and decide whether comparisons are appropriate.
func CompareSummaries(left, right Summary) Comparison {
	return Comparison{
		MeanDeltaMS:      right.MeanMS - left.MeanMS,
		P95DeltaMS:       right.P95MS - left.P95MS,
		FailureRateDelta: right.FailureRate - left.FailureRate,
	}
}
