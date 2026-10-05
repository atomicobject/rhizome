## `src`

`expiry.py` owns the boundary comparison and parsing of persisted expiry
timestamps. The file's public signatures are part of this fixture's contract;
the behavior can be corrected without changing callers or storage.

Read `docs/reference/expiry-policy.md` for the current UTC and boundary rules.
