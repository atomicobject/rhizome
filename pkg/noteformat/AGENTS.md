# Test ownership

- Builtin assembly tests execute both shipped formats in `builtin`; shared source immutability and manifest fingerprints belong to `noteformat`, without provider-local replays.
- Metadata patch tests apply the returned patch and check the requested value. Concurrent projection tests use distinct inputs and check complete results, including literal content.
