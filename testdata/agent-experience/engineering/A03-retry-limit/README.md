# Approved retry limit fixture

This repository contains an approved change to the retry helper. The product
contract is in `docs/specs/retry-limit.md`; execution authority and verification
belong to `docs/efforts/2026-09-07-12-00-retry-limit.md`.

Run the visible checks with:

```sh
python3 -m unittest discover -s tests
```

The neighboring jitter request is deliberately outside this effort.
