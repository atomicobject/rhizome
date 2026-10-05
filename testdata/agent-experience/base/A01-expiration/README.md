# Expiration boundary fixture

This tiny Python package stores an expiration timestamp and answers whether a
record is still valid. The implementation is intentionally small so the task
can be checked through observable behavior.

Run the visible checks with:

```sh
python3 -m unittest discover -s tests
```

The governing rule lives in `docs/reference/expiry-policy.md`. Read the local
context files before changing the implementation.
