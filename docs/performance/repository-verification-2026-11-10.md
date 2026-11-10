---
summary: "Reduce repeated verification work while preserving required gates; an empty-schema web fixture experiment missed its acceptance threshold."
last-verified: 2026-11-10
code-paths:
  - pkg/app/web/server_integration_test.go
  - pkg/testutil/sqlitefixture
---

# Repository verification work

The [quality-gate policy](../engineering/quality-gates.md) now separates focused checks during editing from the required final gates. A successful containing gate can satisfy its included checks while the checked inputs remain unchanged. The [testing policy](../engineering/testing-policy.md) identifies when Go's cached results are useful and when fresh execution is required. Agent waiting guidance prefers completion notifications over fixed sleep polling.

These are workflow changes. The fixture optimization below was measured and reverted because it missed its predefined acceptance threshold.

## Fresh test execution experiment

The candidate replaced only the `semdb.Open` call in `prepareOntologyFixtureVault` with the existing `sqlitefixture.Open` helper. Each caller still created its own temporary directory, authored the same files, published the same ontology, and opened its own writable store. The helper creates a process-local snapshot of the current empty schema through the production open path; it does not share populated fixture data.

An earlier profile attributed 65.6% of the web test package's sampled CPU to `Store.ensureSchema`. That covered all store opens, not just this fixture. The experiment tested whether avoiding repeated migrations in this one helper produced a worthwhile reduction in the entire package's elapsed time.

### Method

- Go 1.27.3 on macOS arm64, with 14 logical CPUs.
- Compile each side once using `go test -c -mod=vendor -tags=fts5`. A Go overlay restored the original helper for the baseline build.
- Alternate five separate runs of each compiled binary from `pkg/app/web`, using `go tool test2json` and identical `-test.count=1 -test.parallel=14 -test.timeout=5m` flags.
- Include a fresh process and its snapshot initialization in every timed run. Compilation is excluded. Neither side can reuse a Go test result.
- Compare the exact sorted test and subtest names and pass, skip, or fail outcomes. Measure child CPU time separately from elapsed time.
- Keep the candidate only if its median elapsed time improves by at least 20% and all outcomes match.

The first direct-binary probe used the repository root as its working directory. Relative fixture paths failed. That setup error was corrected to match the package working directory used by `go test`; the failed probe is excluded below.

Other work was active on the machine. The sides were interleaved to limit drift, and both ranges are reported.

### Results

| Pair | Original elapsed, seconds | Candidate elapsed, seconds | Original CPU, seconds | Candidate CPU, seconds |
| --- | ---: | ---: | ---: | ---: |
| 1 | 32.544 | 27.754 | 47.389 | 22.879 |
| 2 | 33.456 | 27.015 | 51.268 | 25.648 |
| 3 | 33.900 | 28.047 | 52.684 | 26.298 |
| 4 | 31.887 | 27.603 | 50.704 | 26.036 |
| 5 | 32.627 | 27.199 | 49.720 | 24.861 |
| Median | 32.627 | 27.603 | 50.704 | 25.648 |

Elapsed ranges were 31.887 to 33.900 seconds before and 27.015 to 28.047 seconds after. The candidate improved median elapsed time by 15.4% and reduced median CPU time by 49.4%. All ten runs passed the same 540 test and subtest outcomes, with no skips. Temporary helper tests also passed with the race detector, covering independent concurrent copies, persistence after reopening, empty subsequent copies, and equivalence with a fresh schema and its migration proof.

The candidate was faster, but it missed the agreed 20% elapsed-time threshold. The web helper substitution and its temporary test changes were reverted. Production opens, migrations, and committed test fixtures remain unchanged. The experiment does not justify applying the helper to other fixtures or claiming a shipped test-speed improvement.

## Cache limits that remain relevant

Allowing a valid Go test cache hit is different from making fresh tests faster. Every run in the table above executed the tests in a new process.

CI explicitly uses `-count=1` for integration tests because those tests execute a freshly built `rzm` binary whose contents Go's test cache cannot track. The local `integration-packages` and `integration-mixed` Makefile targets currently omit that flag. This mismatch remains unchanged. When verifying a changed executable, use the applicable integration command with `-count=1` and report whether each result was fresh or cached.

Required final gates, fresh migration coverage, credential checks, and CI remain in force. The policy change avoids repeating completed checks on unchanged inputs; it does not make a partial run equivalent to a full gate.
