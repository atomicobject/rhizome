---
name: rhizome-ops
description: Operate Rhizome's AWS team API gateway, report usage by person or in aggregate, investigate logs and performance, and apply authorized runtime settings or guide deployment recovery.
---

# Rhizome operations

Use this repository's `scripts/team-api/ops.py` for reports and bounded runtime settings changes. Report modes are read-only; `settings-update` previews a patch unless `--apply` is supplied. The helper uses the AWS CLI and existing `rzm` admin login, checks the expected AWS account before other reads, and discovers resource names and the HTTPS origin from CloudFormation. It never retrieves secret values, startup configuration, or session-table contents.

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
| Current paid-call switches, daily dollar budgets, limits, and reviewed pricing | `settings` |

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

A report request authorizes reads. Apply changes within the user's authorization. Use `settings-update` for the global paid-call switch, Voyage/Jev switches, provider-wide daily dollar budgets, and default person/global limits. Choose only the requested fields and providers; the helper preserves unrelated settings. Preview with approved values, then supply `--apply` to submit once, compare requested fields with runtime readback, and verify a new matching `config.update` audit entry.

```sh
python3 scripts/team-api/ops.py --account "$TEAM_API_ACCOUNT" \
  --region "$TEAM_API_REGION" --stack "$TEAM_API_STACK" settings-update \
  --paid-calls on --voyage on --typesafe on \
  --voyage-daily-usd "$APPROVED_VOYAGE_DAILY_USD" \
  --typesafe-daily-usd "$APPROVED_TYPESAFE_DAILY_USD" \
  --person-limits "$APPROVED_PERSON_LIMITS" --global-limits "$APPROVED_GLOBAL_LIMITS"
```

`--typesafe` controls Jev. Dollar flags accept nonnegative decimal USD with at most nine fractional digits and convert exactly to integer nanodollars. Preview and settings show both units. Provider budget patches merge only the selected provider; setting Voyage does not replace TypeSafe's budget. Add `--apply` to this command to apply the preview within the authorized scope. Enabling switches while an effective admission limit or provider dollar budget is zero still admits no paid traffic. Set explicit approved budgets and limits before claiming a provider is usable. After an unverified apply outcome, read `settings` and audit before another update; a lost response may follow a committed change, so the helper never resends automatically.

`settings` reports the server's current reviewed `pricing_assumptions`, including model, version, review date, rate, and maximum billable tokens. Jev uses the `jev-latest` alias. The pricing record describes its reviewed billing assumptions; it does not pin a model revision. Investigate an accounting fence or changed provider billing terms before continuing paid traffic. A higher budget cannot clear a provider accounting fence.

Use the admin API for current switches, limits, and eligibility. These values seed DynamoDB on first boot; editing their startup SSM values later does not change current runtime state.

For an authorized spending stop, use `auth admin config --paid-calls off`. It blocks new paid admissions within 15 seconds; already dispatched work can still be charged. To offboard someone, use their Google subject with `auth admin person <sub> --status disabled` and `auth admin revoke-all <sub>`. The runbook allows up to 60 seconds for cached access to expire. Workspace suspension alone is insufficient. Revoke one device with `auth admin revoke <session-id>`.

Legacy limits use `requests,input_bytes,charge_credits`; a zero effective limit admits no paid calls. `--person-limits`, `--global-limits`, `auth admin person <sub> --limits`, and provider dollar budgets cannot exceed their startup ceilings. The runtime API does not expose or change these ceilings. If approved settings require higher ceilings, review a targeted patch to the nonsecret startup configuration in SSM and perform a fresh protected deployment before applying the runtime change. Preserve unrelated startup fields and runtime state. Never write DynamoDB counters/settings directly or treat SSM seed edits as runtime changes. Treat increasing budgets, re-enabling paid calls, and widening eligibility as explicit operating decisions. Follow [the runbook](../../../services/team-api/RUNBOOK.md) for authorized changes and recovery. Preserve the service hostname and durable ledger receipts.

## Deployment and rollback

Read [the runbook](../../../services/team-api/RUNBOOK.md) and the current `.github/workflows/team-api-deploy.yml` before a deployment operation. Production CI retains the main-only environment restriction and GitHub OIDC identity. It deploys automatically without a separate environment reviewer prompt. Use that workflow and its acceptance checks. Supported client publishers automatically deploy and verify the exact release source before client uploads. The workflow executes from trusted public main, but builds the pinned public release SHA; ordinary main pushes do not deploy. Private client releases require matching committed gateway build inputs. Failed or uncertain deployments block publication and retain a receipt for the release resume command. Inspect the exact requested source commit/image and correlated workflow URL, then report the final workflow result and running revision. Do not confuse the dispatcher head SHA with its selected source SHA. A local health check does not replace CI acceptance.

Rollback uses the same protected workflow with a previously published full commit SHA as `image_tag`. It changes code while current runtime settings and durable state remain. Secret or startup configuration changes need a fresh deployment to reach running tasks. IAM/template changes follow their separate reviewed administrative path. The ops helper creates no deployment. Only an explicit `settings-update --apply` changes runtime settings.

This is repository-owned guidance. Keep `.agents/skills/rhizome-ops` and `.claude/skills/rhizome-ops` identical; shared implementation stays in `scripts/team-api/`. It is not an `rzm init` template and does not ship to downstream repositories.
