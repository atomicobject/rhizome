# macOS build network failure at a1b7cd0

The macOS Desktop job for `a1b7cd0da5b1a97097989938fec59027bee3d728` failed while `make build` downloaded dependencies for `services/team-api`. It had not reached Desktop tests.

[Failed job](https://github.com/atomicobject/rhizome/actions/runs/42827728904/job/129869394821).

The retained log reports `lookup proxy.golang.org: no such host` for `gopkg.in/yaml.v3@v3.0.1` and `i/o timeout` resolving that same host for AWS SDK packages. The root CLI build had completed; the separate team-api dependency download caused exit 2.

This is a verified external dependency-resolution failure, not evidence of a Desktop assertion failure. No production, test, timeout, retry or skip change is justified by this log. The current-main integration and image-path correction will receive fresh CI on their new head; readiness still requires that head's checks to pass. The failed old-head job is not treated as a pass.
