# Illustrative policies — Unreleased

These policies require the source implementation of `faultline-bundle evaluate`;
they do not work with v0.1.0 binaries. Build with `make offline`.

- `dependency-budget.json` asks for no request failures, fault mean ≤150 ms and
  fault p95 ≤200 ms; baseline/recovery p95 ≤50 ms. At least five samples per phase.
- `expected-timeouts.json` instead asks for fault failure rate ≥0.9 while
  baseline/recovery have no failures and p95 ≤50 ms. At least five samples per phase.

Both require completed execution and confirmed cleanup. These are deliberately
different questions about the fixed sample topology, not universal defaults or
statistical guarantees. Select thresholds for the planned experiment. A PASS
applies only to the specified conditions and recorded observations.

```sh
./artifacts/bin/faultline-bundle evaluate run.flb --policy examples/policies/dependency-budget.json
```

Bounds are inclusive; failure rates are fractions. Missing/partial/insufficient
data is INDETERMINATE, never zero-filled. See [the full policy contract](../../docs/evidence.md#offline-reliability-gates--unreleased).
