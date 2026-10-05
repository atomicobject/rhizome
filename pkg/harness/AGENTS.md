# Harness tests

- Keep vendor protocol and lifecycle tests in each driver. Shared eventstream tests prove queue behavior; driver tests prove shutdown wiring.
- `cmd/harness_test.go` owns command workflow tests. Do not repeat its happy paths in `harnesscheck`.
- Keep test support only when another test or consumer uses it.
- For populated session options, assert captured CLI arguments or protocol frames.
