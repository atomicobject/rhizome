# Test ownership

- Parser fixtures need distinct source and target identities for every route they claim to prove; assert the extracted identity, not only a count.
- Persistence and negative resolution tests use real stores and seeded candidates so the intended write or rejection is observable.
- Supported parser fixtures must fail on empty symbols. Derive dedupe receipts from a real first delivery, and check performance counts after the scheduled work completes.
