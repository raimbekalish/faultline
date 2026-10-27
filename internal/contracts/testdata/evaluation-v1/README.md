# Evaluation result schema 1: synthetic compatibility contracts

These policies and expected results replay the existing explicitly synthetic
`../v1/complete.json`, `legacy.json` and `canceled.json` evidence. They are not
measured experiments or examples of production reliability. Original version-1
fixtures and the format-1 archive checksum are unchanged.

Fixtures freeze identity/policy/report hashes, field/check ordering, fraction/ms
units, nullable missing measurements, inclusive bounds, lifecycle/sufficiency
reasons, INDETERMINATE precedence and exit codes 0/3/4. Review intentional changes
to expectations; tests do not regenerate fixtures. Whitespace alone is ignored.
