# Updater test clock diagnosis

[macOS Desktop CI](https://github.com/atomicobject/rhizome/actions/runs/42787086837/job/129731111149) failed `a_paused_clock_read_stall_still_times_out` on `cc10aeea35a0a15aa541649ae85125a92a7e3450`. Its expected body-timeout status and lower bound passed; the separate upper bound on total elapsed time failed. The retained CI log has no phase timestamps, so its precise scheduling sequence remains unknown.

## Controlled mechanism

The original test started its paused Tokio clock measurement before the feed lookup and package connection. Its 400 ms bound described the interval after the first package byte, so unrelated setup time could reject a correct 250 ms body timeout.

A diagnostic-only 200 ms pause between feed and package setup produced the exact failed upper-bound assertion with the expected timeout error. Total elapsed was 450 ms while the body interval was 250 ms. This is a deterministic counterexample to the measurement, rather than proof of the unrecorded CI schedule.

The fixture now sends the first package byte's virtual timestamp through one oneshot event. Both paused-clock tests require that event with `try_recv().expect(...)` and measure the body interval from it. A package phase that never starts cannot silently pass. The producer emits the timestamp only after a successful write of headers and the first byte. This records server write time, not client receipt; Tokio's blocking task prevents paused-clock advancement during that phase.

The stalled test keeps `>= 250 ms` and `< 400 ms`; the progressing test keeps `>= 300 ms` and `> 250 ms`. Status, verified-cache, lock-release, overall-timeout and existing retry-behavior assertions remain. No threshold, production timeout, dependency, retry, skip or diagnostic sleep changed in the committed source. Only the test helper and its two callers change.

## Verification and independent review

The Codex GPT-6.1 Sol high implementation lane committed the isolated correction as `70ed4be98b2a3f40655a41ef7c62ee4028ea0bf3`. A fresh Codex GPT-6.1 Sol high source review approved it with no verified issues, inspecting the production updater client and locked plugin callers. Root inspected and integrated it as `f66a9991d`.

The implementation lane passed all eight updater cases, all 145 native Rust tests, fmt, clippy with warnings denied, make check and credential checks. Its isolated native test build omitted absent sidecar packaging inputs through a build-only configuration override. Root separately reproduced the counterexample in the integration checkout and verified exact source restoration afterward:

```text
red: expected exit 101, observed 101
TRACE total=450ms measured=450ms

green: expected exit 0, observed 0
TRACE total=450ms measured=250ms
```

Root artifacts are `/private/tmp/ae-root-updater-counterexample.py`, its red/green logs, and `/private/tmp/ae-root-desktop-check.log`. Root also passed the standard `make desktop-check` with actual sidecars and the synthetic team origin `https://team.example.test`: 151 Desktop UI tests, six script tests, all 145 native Rust tests, fmt, clippy with warnings denied, and the disposable app-instance test. No sidecar-omission override applied to that gate. This is build and process-test evidence; packaged-app updater behavior was not exercised. No installed Rhizome app is replaced. Current-head CI and manual review remain required before PR readiness.
