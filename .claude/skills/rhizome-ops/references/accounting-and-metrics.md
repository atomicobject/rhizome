# Accounting and measurements

## Usage

`usage` reports one UTC admission day at a time, or sums up to 31 inclusive dates. `--user` matches an exact email or immutable Google subject from `people`. Email matching is case insensitive. The identity response maps operational subjects to people; logs contain subjects and session IDs, not email addresses.

`requests` and `input_bytes` count admitted work. They differ from log counts of HTTP attempts. Legacy `reserved_credits` includes both settled credits and conservative holds. `settled_credits` is a component of that value; adding them double counts usage. Credits are configured accounting units, not currency, a provider bill, or necessarily tokens.

`providers` reports Voyage and TypeSafe separately, globally or per person. Each has `requests`, `reserved_nanodollars`, `settled_nanodollars`, `unknown_outcomes`, and `fenced`. One USD equals one billion nanodollars. The helper preserves integer counters and adds exact `reserved_usd` and `settled_usd` strings without floating point. Provider reserved nanodollars already include settled nanodollars; adding those fields double counts. Daily global provider rows include `daily_limit_nanodollars` and an exact `daily_limit_usd`. Provider-wide budgets apply across users; person rows report attributed usage rather than a separate per-person dollar budget.

An accounting fence blocks every new admission for that provider across processes, UTC days, and restarts, even if its budget is raised. Global fence flags report current admission status for every queried date; per-person flags remain historical. `accounting_fenced` and `accounting_fences` summarize global fences across the requested dates, including in user-filtered reports. Provider and person fence flags combine with logical OR across dates, not addition. Keep the affected provider disabled and review its pricing/context bounds before considering a separately reviewed fence repair. No fence-reset operation is exposed, and raising a budget does not repair the accounting assumptions.

Success with known provider usage replaces the conservative reservation with actual credits. Missing provider usage keeps the reservation. A definitive upstream 3xx/4xx/429 rejection releases credits but retains requests and bytes. A connection failure before writing releases requests, bytes, and credits. Ambiguous work after dispatch, including upstream 5xx or a cut-off response, increments `unknown_outcomes` and keeps the full reservation. Never retry unknown work with a new idempotency key or delete receipts merely to recover its budget.

Daily limits and `pricing_assumptions` in reports describe current runtime settings, even for historical dates. Pricing metadata includes the provider, model alias, reviewed pricing version/date, nanodollars per token, and maximum billable tokens. Jev uses `jev-latest`; a recorded pricing version does not pin that model alias. Counters retain their originally booked reservations and settlement rather than being repriced when assumptions change.

The API has no historical limit snapshots, monthly budget, or model-level accounting totals. Use current daily limits for a clearly labeled comparison; sum usage counters over dates rather than inventing a monthly limit. Missing provider rows mean dollar accounting is unavailable, not zero. Dollar usage is conservative gateway accounting under reviewed provider pricing and request bounds; it is not a vendor invoice. Invoice reconciliation requires separately authorized billing evidence. Increasing legacy credits is not a measured increase in actual billed cost.

## Logs and performance

CloudWatch holds the app's bounded JSON events for 30 days. `attributes` contains filter fields and `metrics` contains measurements. The helper follows CloudWatch pagination but has explicit record/page bounds and reports partial results. It excludes malformed/non-app records rather than printing arbitrary process output. Treat user/session identifiers as operational identity data.

`teamapi.request` records every HTTP request, including health and rejection. Its `metrics.duration_ms` and `metrics.authentication_ms` are scalar milliseconds. `attributes` includes `request_id`, `route`, `sub`, `session_id`, `model`, `status`, `upstream_status`, `gateway_code`, `client_version`, and `aborted`. Route values are registered patterns; raw URLs and query strings are absent.

`teamapi.provider_request` overlaps the HTTP event for that work. It has `metrics.duration_ms` with `validation`, `admission_wait`, `reservation`, `upstream`, `settlement`, and `total`. Its attributes include route, model, upstream status, gateway error code, user/session, and request ID. The free TypeSafe model-list route also produces this event. The helper selects only the two paid POST routes for provider timing/count reports and excludes health from HTTP reports. Count either HTTP or provider work; adding those event families double counts requests.

Percentiles use nearest rank and report sample counts. Missing or truncated measurements are excluded, not assigned zero. Report the sample size, time window, and completeness with every percentile. Compare the same route/model and workload when investigating a regression. Separate upstream time from gateway authentication, admission wait, reservation, and settlement time. A partial or quiet sample cannot establish capacity or an uptime guarantee.

## AWS capacity

The helper requests five-minute datapoints for ECS CPU/memory utilization, ALB target latency, ALB and target 5xx counts, ALB request counts, and DynamoDB read/write throttle events. It returns AWS datapoints with their dimensions and units. Count metrics use `Sum`; utilization and latency use `Average`, `Maximum`, and `SampleCount`. These are platform measurements, distinct from the app's per-request phase timings. ALB latency includes provider time. Keep seconds and milliseconds labeled.

Missing datapoints mean unavailable or not emitted, never zero. In particular, AWS may omit sparse error counters. The deployment template configures neither Container Insights nor application custom metrics, dashboards, live alarms, pager integration, or ALB access logs. Verify live configuration before claiming any of those exists. Add alerts or instrumentation only as authorized work; metric availability is not evidence that an alarm is enabled.
