## Expiration fixture

This repository owns a small UTC expiration helper. The implementation in
`src/expiry.py` is bound to the current policy at
[[docs/reference/expiry-policy|the expiry policy]].

- Validity ends exactly at the expiry instant.
- Persisted timestamps are interpreted as UTC-aware values.
- Keep the public helper signatures and persisted representation stable while
  repairing the behavior.

`docs/history/expiry-policy-local-time.md` is retained historical context. Its
local-time rule is explicitly superseded by the current policy.
