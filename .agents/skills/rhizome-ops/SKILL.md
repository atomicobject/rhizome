---
name: rhizome-ops
description: Operate Rhizome's AWS team API gateway, report usage by person or in aggregate, investigate logs and performance, and guide authorized access changes or deployment recovery.
---

# Rhizome operations

Use this repository's `scripts/team-api/ops.py` for read-only reports. It uses the AWS CLI and existing `rzm` admin login, checks the expected AWS account before other reads, and discovers resource names and the HTTPS origin from CloudFormation. It never retrieves secret values, startup configuration, or session-table contents.

Run from the repository root. Obtain the account, region, and stack from the user's deployment context or protected GitHub `production` variables, `TEAM_API_AWS_REGION`, `TEAM_API_STACK`, and the account in `TEAM_API_DEPLOY_ROLE_ARN`. Confirm the reported identity matches that target. Use the active AWS login; if it has expired, have the user complete `aws login`. Keep credentials out of chat, command arguments, Git, and reports.

```sh
python3 scripts/team-api/ops.py --account "$TEAM_API_ACCOUNT" \
  --region "$TEAM_API_REGION" --stack "$TEAM_API_STACK" status
```

Global flags precede the mode. The helper defaults to this checkout's absolute `scripts/rzm` launcher and disables repository delegation. Pass `--rzm /absolute/path/to/rzm` for another explicitly selected build. If the checkout binary is missing, use an installed release build explicitly or build this checkout. Read `--help` for current flags. All helper output is JSON.

## Choose the report

Append one of these modes after the target flags:

| Request | Mode and options |
| --- | --- |
| Is production healthy? Which image is serving? | `status` |
| Usage today, globally and by person | `usage` |
| Usage for a UTC date range | `usage --start 2026-10-30 --end 2026-11-02` |
| Usage for one person | `usage --user person@example.com` or `usage --user <Google-sub>` |
| Recent operational logs | `logs --hours 1` |
| Trace a request | `logs --hours 2 --request-id <id>` |
| Investigate one person's requests | `logs --hours 2 --user person@example.com` |
| Request errors and phase timings | `performance --hours 1`, optionally `--user <email-or-sub>` |
| ECS, ALB, and DynamoDB measurements | `capacity --hours 1` |

State the actual account, stack, UTC window, and completeness in the report. Log and capacity windows are at most 24 hours. Log retrieval follows pagination up to the requested `--max-events`, default 5000 and maximum 10000. `complete: false` means counts and percentiles cover only the retrieved portion. Narrow a partial window before presenting a complete total. No matching events, missing metrics, and denied reads are separate findings.

`status` checks HTTPS health and compares the requested CloudFormation task revision with the successful Express canary, current ECS service revision, and running task/image. A 200 health response proves neither Google sign-in nor provider availability. Report a failed revision check even when health is green.

Read [references/accounting-and-metrics.md](references/accounting-and-metrics.md) before interpreting usage, costs, performance, or capacity.

## People, sessions, configuration, and audit

Use the stack's `Endpoint` as the explicit `--server` HTTPS origin. Use an existing admin session; when one is missing, ask the user to complete `rzm auth login --server "$TEAM_API_ORIGIN"`. The helper never signs in automatically or prints tokens.

```sh
./scripts/rzm auth admin people --server "$TEAM_API_ORIGIN" --json
./scripts/rzm auth admin sessions --server "$TEAM_API_ORIGIN" --sub "$GOOGLE_SUB" --json
./scripts/rzm auth admin config --server "$TEAM_API_ORIGIN" --json
./scripts/rzm auth admin audit --server "$TEAM_API_ORIGIN" --limit 100 --json
```

`people` supplies the immutable Google subject for identity changes and joining logs to email. Session lists contain device/client metadata and identifiers, never credentials. `audit` defaults to 100 entries and caps at 1000; it is a recent-change report, not an unlimited history export.

## Incident changes

A report request authorizes reads. Apply changes only within the user's authorization, and verify the changed state and audit afterward. Use the admin API for current switches, limits, and eligibility. These values seed DynamoDB on first boot; editing their startup SSM values later does not change current runtime state.

For an authorized spending stop, use `auth admin config --paid-calls off`. It blocks new paid admissions within 15 seconds; already dispatched work can still be charged. To offboard someone, use their Google subject with `auth admin person <sub> --status disabled` and `auth admin revoke-all <sub>`. The runbook allows up to 60 seconds for cached access to expire. Workspace suspension alone is insufficient. Revoke one device with `auth admin revoke <session-id>`.

Limits use `requests,input_bytes,charge_credits`; a zero limit admits no paid calls. `config --person-limits`, `config --global-limits`, and `person <sub> --limits` cannot exceed startup ceilings. Treat increasing limits, re-enabling paid calls, and widening eligibility as explicit operating decisions. Follow [the runbook](../../../services/team-api/RUNBOOK.md) for authorized changes and recovery. Preserve the service hostname and durable ledger receipts.

## Deployment and rollback

Read [the runbook](../../../services/team-api/RUNBOOK.md) and the current `.github/workflows/team-api-deploy.yml` before a deployment operation. Use the protected CI workflow and its acceptance checks. Inspect the exact requested commit/image and report the final workflow result and running revision. A local health check does not replace CI acceptance.

Rollback uses the same protected workflow with a previously published full commit SHA as `image_tag`. It changes code while current runtime settings and durable state remain. Secret or startup configuration changes need a fresh deployment to reach running tasks. IAM/template changes follow their separate reviewed administrative path. The read-only helper creates no deployment and alters no AWS resource.

This is repository-owned guidance. Keep `.agents/skills/rhizome-ops` and `.claude/skills/rhizome-ops` identical; shared implementation stays in `scripts/team-api/`. It is not an `rzm init` template and does not ship to downstream repositories.
