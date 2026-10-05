# TypeSafe Go client

`pkg/typesafe` owns the HTTP contract for TypeSafe's System One API, including Jev. It uses only the Go standard library. It does not discover notes, resolve Rhizome credentials, choose automation thresholds, or change the agent catalog. Keeping those decisions with callers lets experiments use this client without committing Rhizome to a particular workflow.

## Native Go API

`NewClient(apiKey, options...)` creates a reusable, concurrent client. Pass native structs, maps, slices, or text as state. Question variants are distinct Go structs with automatic wire discriminators:

| Method | Question | Return type |
| --- | --- | --- |
| `Choose` | `Choice` | `Result[ChoiceAnswer]` |
| `Score` | `Score` | `Result[ScoreAnswer]` |
| `Check` | `Noul` | `Result[NoulAnswer]` |
| `Evaluate` | `Request` containing mixed named questions | `Response` |
| `EvaluateBatch` | Up to 128 identified requests, 1..32 workers | Ordered `[]BatchResult` |
| `Models` | None | `ModelsResponse` |

Every single-question result preserves its typed answer, model, token usage, and request ID. Mixed responses offer `Choice(id)`, `Score(id)`, and `Noul(id)` accessors, which return an error for missing IDs or the wrong answer type. `Answers` also supports a Go type switch. `Request.UnmarshalJSON` decodes wire discriminators into native question structs and validates them before replacing the receiver.

Instructions and criterion descriptions accept native JSON-compatible values: strings, objects (including Go structs), arrays, or nil. `Choice.Criteria` is a map of option names to descriptions. `Score.Criteria` is an ordered slice, with levels numbered from zero. `Noul.Criteria` optionally describes true and false. The flexible description values do not replace the typed question and answer contracts.

See [compiled examples](example_test.go) for single-question and mixed-batch calls. For a Rhizome caller, resolve `TYPESAFE_API_KEY` through `vaultconfig.ResolveValue` from the environment or private user configuration, and pass that value to `NewClient`. The client itself never reads environment or configuration files. Shared keys can be imported at setup with `rzm credentials import`; no release embeds provider credentials.

## Transport and validation

- Default endpoint: `https://api.typesafe.ai`; default model: `jev-latest`. Use `WithModel` or `Request.Model` to pin a model for repeatable evaluations.
- A call has a 30-second total budget, including retries. `WithTimeout` changes it, and an earlier caller context deadline wins. Cancellation interrupts both requests and retry waits.
- Retry HTTP 408, 429, and 5xx responses, at most twice by default. Honor `retry-after-ms`, `Retry-After` seconds, and HTTP dates. Otherwise use exponential backoff with jitter. If the server delay exceeds the remaining budget, return the HTTP error immediately. `WithMaxRetries(0)` disables retries.
- Ambiguous transport failures are not retried automatically, since replay may duplicate billed inference. Error bodies remain available on `APIError` for explicit inspection, but are excluded from its error string. The API key is redacted from the stored body. Redirects are not followed.
- Responses are limited to 16 MiB by default, adjustable with `WithMaxResponseBytes`. `WithHTTPClient` supports custom transports and connection pooling without changing the supplied client's redirect policy.
- Validate answer IDs, kinds, required fields, probability ranges, normalized distributions, and option/level coverage before returning results. Missing fields never turn into valid zero values. Unknown additional fields are accepted. Probability sums allow a small service-side rounding margin around one.
- The client snapshots a request once for retries. Callers must not modify request maps or slices while a call is encoding them.

`errors.As` distinguishes `*APIError` (an HTTP failure) from `*ResponseError` (malformed service output). `errors.Is` preserves context cancellation and deadline errors. Confidence is a distribution statistic; interpreting it and choosing thresholds belongs to each experiment.

## Verification and sources

Run `go test -race ./pkg/typesafe`. Offline tests use HTTP fixtures and synthetic credentials. Explicitly opt into a small live contract check with `TYPESAFE_LIVE_TEST=1 go test ./pkg/typesafe -run '^TestLiveSystemOne$' -count=1 -v`; `TYPESAFE_API_KEY` must already be in the environment. That test sends only synthetic text and checks all three primitives with structured descriptions.

The live contract was checked against `jev-1.13.0` on 2026-09-17. The [HTTP reference](https://docs.typesafe.ai/api), [structured questions](https://docs.typesafe.ai/primitives/advanced), [SDK question types](https://docs.typesafe.ai/sdk/python/api/types/questions), and [model documentation](https://docs.typesafe.ai/models) define the upstream contract. The advanced and SDK docs permit structured descriptions and optional instructions more broadly than the HTTP reference's field tables; the client preserves those supported forms.

Batch results preserve each item ID, attempts, request ID, typed response or failure, and execution status. A shared per-batch throttle pauses new attempts after HTTP 429; the normal retry policy remains the only retry loop. Cancellation retains completed results and distinguishes never-started items from uncertain attempted work. Callers own persistence and decide whether to retry known failures; successful and uncertain items are never automatically replayed as a batch.
