# SSE fixture repair

Scope: only pkg/app/web/agent_test.go. Parent 22c874a4b22d93d9020609ace73636a8200299ff.

The original fixture starts a 20 ms request deadline before invoking the handler. handleAgentEvents calls Service.GetSession, then newAgentEventStream/EventsAfter, before the first Flush. Service.GetSession performs context-aware store session, message, and event reads. Store.GetSession uses QueryRowContext; Store.EventsAfter uses QueryContext. sessionError preserves context errors and writeAgentError maps them to BAD_REQUEST/400.

Controlled red: the old fixture waited on ctx.Done() immediately before invoking the unchanged handler. Its retained response was HTTP 400, {"error":"context deadline exceeded","code":"BAD_REQUEST"}. The test failed its HTTP 200 assertion. No production code changed. See sse-admission-red.log and sse-agent-test-original.go. The injected line was `<-ctx.Done()` after setting Last-Event-ID; the status assertion included response.Body.String().

Controlled green: the corrected fixture used httptest.NewServer with a temporary handler wrapper that waited for a separate 20 ms setup context to expire before invoking the real handler with its still-live request context. The HTTP consumer received headers after admission, then started its unchanged 20 ms collection timer and canceled the request. It passed HTTP 200, context.Canceled collection termination, third persisted ID inclusion, and first two ID exclusions with Last-Event-ID overriding after=0. See sse-admission-green.log and sse-agent-test-delayed-green.go. The wrapper and delay were removed before focused checks.

The corrected permanent fixture uses a real loopback HTTP consumer and time.AfterFunc(20*time.Millisecond, cancel) only after Client.Do returns the response headers. The response body is retained in both status and collection-error assertions. Deferred cancellation, body close, timer stop, and httptest server close release resources before service cleanup. No retries, threshold increases, skips, production changes, or permanent diagnostic delays.

Focused command: GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/web -run 'TestAgent(Event|SessionReadDeleteAndSSE)' -count=1 -v. Passed. See sse-focused.log.

Environment: the first controlled-green invocation was blocked by the sandbox's loopback bind restriction. The same proof and subsequent HTTP tests ran outside the sandbox with the user's authorization. npm ci installed the missing checkout-local web dependencies before make check.

Uncertainty: the original query-ownership-check.log records only expected 200/actual 400, with no error body. These experiments establish the hypothesized fixture mechanism, not the exact original error or which admission read encountered it. No evidence establishes a production bug.

Validation: make check passed with exit 0. See sse-make-check.log. git diff --check passed. The final diff was inspected and contains only the fixture repair; diagnostic delay and instrumentation are absent. No pushes, PR changes, watches, merge, or Dev installation performed. Root retains external delivery and independent review.

Final local commit: f8ab0d305 on t3/stabilize-sse-test. Worktree clean after commit. Credential pre-commit hook passed.

## Independent fixture review

A fresh codex/gpt-6.1-sol high review approved f8ab0d305 with no verified blocking findings. It inspected the handler, service/store callers, context behavior and cleanup. HTTP context values do not cross the connection, but these admission reads use the service store and environment. Cleanup cancels and closes the body before the server; service cleanup follows handler shutdown. The reviewer inspected retained red/green logs without rerunning tests. The 20 ms body window retains its scheduling limit; no absolute event-delivery guarantee is claimed. The exact original 400 cause remains unconfirmed because its body was discarded.
