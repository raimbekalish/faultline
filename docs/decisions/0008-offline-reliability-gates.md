# ADR 0008: Conservative, deterministic offline reliability gates

- Status: accepted for the unreleased Milestone 7 implementation
- Date: 2026-10-27

Add `faultline-bundle evaluate RUN.flb --policy POLICY.json`. Keep archive/semantic
validation in `bundle`/`record`, strict Unicode/duplicate handling in `jsonstrict`,
policy loading in a small adapter, and decisions in the pure `bundleevaluate`
package. No new dependency, migration, report/specification change or network call.
The full CLI's existing bundle delegation also reaches the same implementation.

Policy version 1 explicitly requires `require_complete: true` and
`require_cleanup: true`; there is no opt-out. At least one named phase must have a
threshold, and configured phases cannot be empty. Up to five conditions per phase
plus two lifecycle requirements bound a result to 17 checks. This is a fixed
grammar, not a workflow/expression engine. Bounds are inclusive float64 comparisons
without epsilon or display rounding. Failure rates are fractions. The existing
engine summarizer supplies metrics after raw-evidence validation; a stored mean's
permitted four-ULP roundoff cannot change a decision at an exact threshold. This
does not modify or repair the report, or change existing comparison semantics.

Completed execution, confirmed cleanup and fully measured requested phases are
prerequisites for conclusive checks. `min_samples` (1–100, matching the existing
sample cap) is a sufficiency requirement. Fewer samples yield INDETERMINATE, not a
performance failure. Missing metric values are null; an observed zero remains
zero. Partial values remain inspectable, but no threshold verdict is inferred
from them. Failed/canceled runs remain indeterminate even when some phases are
fully measured. Lifecycle results explain unsuitable evidence. INDETERMINATE takes
precedence over FAIL, so a conclusive failure in another phase cannot conceal a
missing prerequisite. PASS means only that this policy's explicit requirements
hold for these recorded samples, not that unrequested metrics are acceptable.

Policy input is at most 8 KiB. Reject unknown/case-aliased fields, all null fields,
simple-fold/escaped duplicate keys, invalid Unicode, non-finite/negative/out-of-range
numbers, float underflow to zero and contradictory min/max failure bounds. Allow
explicit boundary rates 0/1 and zero durations. Structural emptiness is rejected;
the evaluator does not second-guess a user's deliberately permissive threshold.
Both decoded and programmatically constructed policies are validated before use.

Local policy reads follow the bounded regular-file/identity pattern of bundle
reads. On the four qualified Darwin/Linux targets, no-follow/nonblocking open
also prevents a last-component symlink or FIFO substituted between stat/open.
Parent directories use ordinary OS path resolution; callers must trust their
selected directory. No includes, URL loads, directory traversal from policy data,
environment interpolation, output files or database/collector operations exist.
Other OS builds retain regular-file/identity checks, but are unqualified. Reads
and evaluation check cancellation cooperatively; arbitrary stalled kernel reads
or output writers cannot be forcibly interrupted. No unbounded goroutine is used
to pretend otherwise. A concurrent in-place file writer is not an attested policy
snapshot; use stable local inputs. The result hashes the validated typed policy.

Result schema 1 has fixed field/phase/check ordering, literal thresholds, observed
values and units, machine-readable reasons, a canonical policy hash and original
report/experiment identifiers. It contains no paths, clocks or ambient provenance.
The policy hash is SHA-256 of Go's JSON encoding of the validated typed policy
(including its fixed field order); it is not RFC 8785 or publisher authentication.

Only evaluate adds verdict exit codes: 0 PASS, 3 FAIL, 4 INDETERMINATE. Existing
1 input/I/O/cancellation/output-error and 2 usage meanings remain unchanged.
Valid verdicts write JSON to stdout with empty stderr. Errors before publication
write stderr and no result; a broken stdout may already contain a partial write.
Output errors override verdict codes. Existing verify/inspect/compare outputs,
format-1 bundle bytes and comparison exit semantics remain unchanged. No new
dashboard/API behavior, released binary, GitHub Actions or release publication.
