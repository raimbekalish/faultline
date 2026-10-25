# ADR 0006: Descriptive offline comparison with explicit eligibility

- Status: accepted
- Date: 2026-10-25

Add `faultline-bundle compare LEFT.flb RIGHT.flb` with a separate version-1 JSON
comparison result. Both files independently pass the existing bounded archive,
checksum and semantic validator. Bundle format 1 is unchanged. The pure comparison
package consumes validated evidence, not paths; CLI owns sequential file reads and
presentation. No database, network, current-machine capture or new dependency.

Extract existing right-minus-left mean/p95/failure-rate arithmetic into one pure
engine helper used by engine reports, history, validation and offline comparison.
Do not re-summarize or repair recorded measurements in the comparison layer.

All three phases show each side's observed/configured counts and recorded summary.
No observations means a null summary, not zero latency. Failed/canceled, incomplete,
unconfirmed-cleanup or conflicting-identity pairs have no cross-run deltas. Their
side measurements remain inspectable. Valid non-comparable pairs produce JSON and
exit 0; malformed/unsupported inputs, I/O or cancellation exit 1; usage exits 2.

Completed, cleaned full reports permit descriptive deltas even when configuration
or provenance differs/is unavailable. Matching recorded conditions requires equal
spec fields/hash and equal recorded revision, runner OS/arch, Go version,
methodology, tracing state, Docker version, sample/Toxiproxy IDs and Toxiproxy
reference. Jaeger facts are required only when tracing is explicitly enabled on
both sides. Missing sample reference/Jaeger-off facts remain unknown in output;
they are not required conditions. Dirty/development/test revision labels downgrade
the assessment. Equality is literal, including revision strings and image IDs.

Known differing methodologies block deltas; current format support recognizes only
`serial-http-nearest-rank-v1`, so other methodologies already fail bundle validation.
Missing legacy methodology is unknown and permits descriptive arithmetic with a
warning. Legacy revision/Go version and explicit telemetry flags may be compared
only when present in the original report. No missing fact is filled from this
machine or inferred from an absent collector/image field.

Classification is `recorded_conditions_match`, `descriptive_only` or
`not_comparable`. None establishes a controlled experiment or statistical
significance. Reusing an identity with changed report bytes/history metadata blocks
comparison. Identical recorded runs are labeled as such; zero deltas are not
independent replication. Distinct identifiers do not prove independent execution.

Absolute changes are signed right-minus-left values in ms or failure-rate percentage
points. Relative percentages use `(right-left)/left*100` only for a positive
reference and finite representable result. Zero-reference (including 0/0) and
floating-point overflow yield null plus an explicit reason, never infinity or an
invented zero. Full summaries retain existing fraction-valued failure rates.

Results use fixed field/phase/property ordering, no paths/current timestamps, and
logical identities plus original report SHA-256. They identify recorded payloads,
not whole ZIP-container bytes or authenticated executions. No dashboard expansion,
new experiment behavior, signing, import or automatic causal analysis is added.
