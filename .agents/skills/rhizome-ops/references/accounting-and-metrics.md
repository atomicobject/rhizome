# Accounting and measurements

## Usage

`usage` reports one UTC admission day at a time, or sums up to 31 inclusive dates. `--user` matches an exact email or immutable Google subject from `people`. Email matching is case insensitive. The identity response maps operational subjects to people; logs contain subjects and session IDs, not email addresses.

`requests` and `input_bytes` count admitted work. They differ from log counts of HTTP attempts. `reserved_credits` includes both settled credits and conservative holds. `settled_credits` is a component of that value; adding them double counts usage. Credits are configured accounting units, not currency, a provider bill, or necessarily tokens.

Success with known provider usage replaces the conservative reservation with actual credits. Missing provider usage keeps the reservation. A definitive upstream 3xx/4xx/429 rejection releases credits but retains requests and bytes. A connection failure before writing releases requests, bytes, and credits. Ambiguous work after dispatch, including upstream 5xx or a cut-off response, increments `unknown_outcomes` and keeps the full reservation. Never retry unknown work with a new idempotency key or delete receipts merely to recover its budget.

Daily limits in reports are the current runtime limits, even for historical dates. The API has no historical limit snapshots, monthly budget, or per-provider/model accounting split. Use current daily limits for a clearly labeled comparison; sum usage counters over dates rather than inventing a monthly limit. Provider invoice reconciliation requires separately authorized billing evidence. Increasing credits is not a measured increase in actual billed cost.

## Logs and performance

CloudWatch holds the app's bounded JSON events for 30 days. `attributes` contains filter fields and `metrics` contains measurements. The helper follows CloudWatch pagination but has explicit record/page bounds and reports partial results. It excludes malformed/non-app records rather than printing arbitrary process output. Treat user/session identifiers as operational identity data.

`teamapi.request` records every HTTP request, including health and rejection. Its `metrics.duration_ms` and `metrics.authentication_ms` are scalar milliseconds. `attributes` includes `request_id`, `route`, `sub`, `session_id`, `model`, `status`, `upstream_status`, `gateway_code`, `client_version`, and `aborted`. Route values are registered patterns; raw URLs and query strings are absent.

`teamapi.provider_request` overlaps the HTTP event for that work. It has `metrics.duration_ms` with `validation`, `admission_wait`, `reservation`, `upstream`, `settlement`, and `total`. Its attributes include route, model, upstream status, gateway error code, user/session, and request ID. The free TypeSafe model-list route also produces this event. The helper selects only the two paid POST routes for provider timing/count reports and excludes health from HTTP reports. Count either HTTP or provider work; adding those event families double counts requests.

Percentiles use nearest rank and report sample counts. Missing or truncated measurements are excluded, not assigned zero. Report the sample size, time window, and completeness with every percentile. Compare the same route/model and workload when investigating a regression. Separate upstream time from gateway authentication, admission wait, reservation, and settlement time. A partial or quiet sample cannot establish capacity or an uptime guarantee.

## AWS capacity

The helper requests five-minute datapoints for ECS CPU/memory utilization, ALB target latency, ALB and target 5xx counts, ALB request counts, and DynamoDB read/write throttle events. It returns AWS datapoints with their dimensions and units. Count metrics use `Sum`; utilization and latency use `Average`, `Maximum`, and `SampleCount`. These are platform measurements, distinct from the app's per-request phase timings. ALB latency includes provider time. Keep seconds and milliseconds labeled.

Missing datapoints mean unavailable or not emitted, never zero. In particular, AWS may omit sparse error counters. The deployment template configures neither Container Insights nor application custom metrics, dashboards, live alarms, pager integration, or ALB access logs. Verify live configuration before claiming any of those exists. Add alerts or instrumentation only as authorized work; metric availability is not evidence that an alarm is enabled.
