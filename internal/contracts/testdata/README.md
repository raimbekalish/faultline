# Synthetic compatibility contracts

All values here are invented test inputs, **not measured experiments**. IDs and
2026-01-01 timestamps are fixed so identity relationships, ordering and original
report hashes are reviewable; they describe no person or machine. Versions and
platform labels exercise syntax only. No environment is inferred during tests.

`v1/*.json` evidence inputs deliberately include complete provenance without launch
metadata, legacy missing provenance, and canceled/partial evidence with unconfirmed
cleanup. Expected inspect/verify/compare JSON freezes field names, units, nulls,
reason ordering and semantics. Whitespace is ignored, not property order. The
format-1 SHA freezes the deterministic archive built from the complete fixture,
including its original report bytes. These are source fixtures, not `.flb` binaries.
The unsupported manifest must be rejected; corrupt/header/Unicode cases live in
focused parser tests. No automatic fixture-update switch is provided: changes need
explicit review and must not be used to hide incompatible behavior.

API/history contracts use these same inputs in actual temporary SQLite files.
Only fixture IDs/times are fixed; no wall-clock time, paths or host facts are snapped.
