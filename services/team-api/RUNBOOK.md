# Team API operations

The pilot runs one ARM64 Fargate task on ECS Express Mode, with its AWS-issued HTTPS hostname. A deploy briefly overlaps old and new tasks. DynamoDB keeps daily reservations exact; in-process concurrency can briefly double. Store-backed concurrency slots are required before rollout beyond the pilot. Live IAM, canary, slow-request, cancellation, and recovery drills remain deployment acceptance work.

## First deployment

Drew performs these steps in order. Account IDs, resource names, the endpoint, Google client metadata, people, and budget decisions stay in AWS or protected GitHub environment variables. Do not commit a filled configuration or parameter file.

1. Merge the reviewed change to protected `main`. Configure branch protection with required checks. Complete bootstrap and configuration before the first deployment.
2. In the target region, confirm the default VPC has public subnets with internet routing and the `default` ECS cluster exists. Express uses those public subnets and public task IPs, with no NAT. The ECS service-linked role must exist; the target account's spike already created it. Record the public subnet CIDRs for `trusted_proxy_cidrs`. Confirm ARM64 Fargate capacity and GitHub's `ubuntu-24.04-arm` runner are available.
3. Using admin credentials outside agent sessions, create the stack once, with serving disabled. Set `CreateGitHubOidcProvider=true` only when the account does not already have `token.actions.githubusercontent.com`. An existing provider must allow audience `sts.amazonaws.com`. Never delete a shared provider.

   ```sh
   github_subject="$(gh api repos/atomicobject/rhizome/actions/oidc/customization/sub --jq '.sub_claim_prefix + ":environment:production"')"
   aws cloudformation create-stack --region <region> --stack-name <stack-name> \
     --template-body file://services/team-api/deploy/template.yml \
     --capabilities CAPABILITY_IAM \
     --parameters ParameterKey=EnableService,ParameterValue=false \
       ParameterKey=CreateGitHubOidcProvider,ParameterValue=<true-or-false> \
       ParameterKey=GitHubOidcSubject,ParameterValue="$github_subject" \
     --stack-policy-body file://services/team-api/deploy/stack-policy.json
   aws cloudformation wait stack-create-complete --region <region> --stack-name <stack-name>
   aws cloudformation describe-stacks --region <region> --stack-name <stack-name> \
     --query 'Stacks[0].Outputs'
   ```

   Save the outputs in the private operations record. Bootstrap creates ECR, the protected DynamoDB table, placeholder secrets and config, roles, logs, and an unlaunched task definition. It does not start a gateway against placeholders. Do not attach an administrator CloudFormation service role to this stack; workflow updates use the restricted caller role. The stack policy permits only task revisions and in-place gateway updates. It targets resource types because CloudFormation rejects a policy naming the conditional gateway's logical ID while `EnableService=false`.
4. In Secrets Manager, replace the three placeholders with the Voyage key, TypeSafe key, and Google Desktop OAuth client secret. Use the console or an approved secret-delivery tool. Never put their values in shell arguments, CI, CloudFormation parameters, or Git. The task execution role reads these three secrets; the application task role cannot read secrets. Default AWS-managed encryption is used, so no customer-managed KMS grant is required.
5. In SSM Parameter Store, replace `ConfigParameter` with a complete production JSON configuration. It is an Advanced String parameter to fit up to 8 KiB. It contains no provider keys or OAuth secret. Use the template below as a shape, replacing all synthetic metadata. Set `state.table` to `TableName` and `state.region` to the target region. `listen`, `data_dir`, and `diagnostics_output` must have the shown values for this image. Set the exact ALB subnet CIDRs, not `0.0.0.0/0` or a whole VPC. Production supports the approved Workspace domain `atomicobject.com`; the example uses reserved synthetic metadata. Name at least one actual admin by email, include pilot participants in the allowlist, and choose explicit per-person/global limits and ceilings. The zero defaults below deliberately admit no paid traffic until an admin sets approved limits.

   ```json
   {
     "mode": "production",
     "listen": ":8080",
     "data_dir": "/data",
     "diagnostics_output": "stdout",
     "trusted_proxy_cidrs": ["192.0.2.0/24"],
     "state": {"backend": "dynamodb", "table": "replace-with-TableName", "region": "us-east-2"},
     "issuer": "https://accounts.google.com",
     "client_id": "synthetic.apps.googleusercontent.com",
     "scopes": "openid email",
     "accepted_domains": ["example.com"],
     "hd_hint": "example.com",
     "admins": ["operator@example.com"],
     "eligibility_mode": "allowlist",
     "allowlist": ["operator@example.com"],
     "person_limits": {"requests": 0, "input_bytes": 0, "charge_credits": 0},
     "global_limits": {"requests": 0, "input_bytes": 0, "charge_credits": 0},
     "max_person_limits": {"requests": 100, "input_bytes": 1000000, "charge_credits": 100000},
     "max_global_limits": {"requests": 300, "input_bytes": 3000000, "charge_credits": 300000},
     "provider_daily_nanodollars": {"voyage": 0, "typesafe": 0},
     "max_provider_daily_nanodollars": {"voyage": 0, "typesafe": 0},
     "concurrency": {"voyage_concurrency": 4, "typesafe_concurrency": 4}
   }
   ```

   Limits, allowlists, and switches seed DynamoDB only on first startup. Later changes use the admin API. Admin identities, domains, and ceilings remain startup configuration. Task clocks must track UTC. Receipts have no TTL and must not be pruned.
6. Create the GitHub environment `production` with a deployment branch rule allowing only `main`. Deployments run automatically without required environment reviewers, as Drew requested. Keep `main` protected. Set environment variables `TEAM_API_AWS_REGION`, `TEAM_API_STACK`, and `TEAM_API_DEPLOY_ROLE_ARN` from the bootstrap outputs. These are references, never secret values. The role's OIDC trust requires audience `sts.amazonaws.com` and the exact `GitHubOidcSubject` from bootstrap. Current GitHub repositories include immutable owner and repository IDs in their subject prefix; do not substitute the older name-only format or a wildcard. Inspect customized subject claims separately if the repository does not use GitHub's default template. GitHub environment branch protection supplies the main-only restriction because the environment OIDC subject does not also contain a branch.
7. Run **Deploy team API** on `main`, with `image_tag` empty. It tests the gateway, builds ARM64, pushes an immutable commit-SHA tag, and updates the existing stack using its previous template. The first update enables the service. Subsequent updates must never set `EnableService=false`, change its logical ID, `Cluster`, `ServiceName`, `InfrastructureRoleArn`, or add/change `Tags`. CloudFormation marks those service properties as replacement operations. `Retain`, a stack policy, and the deployment change-set guard protect the hostname.
8. Confirm the workflow reports the requested canary revision successful, one running task, ALB settings verified, task ingress restricted to ALB security groups, and `HTTPS /healthz: 200`. Save the `Endpoint` as the canonical root HTTPS origin. An initial bootstrap failure before service creation can be retried; if it retains a partially created service, inspect and import or recover that service rather than creating another hostname casually.
9. Sign in to the endpoint from a release build with `rzm auth login --server <https-origin>`. Check admin access, set approved limits, and perform the pilot's live offboarding, kill-switch, retry, cancellation, slow-request, deployment, rollback, and memory drills. A health check proves startup and lease recovery, not provider availability or Google sign-in. Hold broader rollout until the spec's remaining gates pass.
10. Supply `RZM_INTERNAL_TEAM_API_ORIGIN=<https-origin>` in the private release environment alongside `RZM_INTERNAL_S3_BUCKET`. GoReleaser embeds it as `pkg/teamapi/session.DefaultOrigin` only for internal builds. Public builds keep an empty default. Changing this input does not retire the legacy bundle or rotate any key; those remain later rollout work.

## Deploy and rollback

Supported client release commands automatically dispatch **Deploy team API** on trusted public `main` and wait before publishing client artifacts. Server source is the exact selected public `release` commit, not the dispatcher main commit. Ordinary pushes to main no longer deploy. The production environment still permits only main; its OIDC restrictions are unchanged. Actions follow the repository's existing version-tag convention. No long-lived AWS keys are configured; CI receives an OIDC session. The role can push only this repository, update only this stack and service, pass only these roles, and operate on this task-definition family. ECS task registration is scoped to this stack's family. AWS requires `*` for ECR authorization, `DescribeTaskDefinition`, and the network describe operations. Task-definition revisions are retained, so the deploy role needs no unrestricted deregistration permission. ALB writes are limited to this account/region's Express-managed ALBs by the `AmazonECSManaged=true` tag; their generated ARNs are unknown at bootstrap. Review that residual scope with IAM Access Analyzer during the live acceptance pass.

Source deployment requires paired full-SHA `source_commit` and random hex `request_id` inputs; normal release commands choose them automatically. It checks the source against trusted public main history, tests/builds a separate source checkout, and executes `deploy.py` from trusted main. Do not select a private commit or run an old deployment helper. See [release publication gating](../../docs/RELEASING.md#gateway-deployment-before-publication) for private/public source agreement, receipts and retries.

The workflow uses the previous CloudFormation template and values, changing only image tag, deployment ID, and initial enablement. CloudFormation also needs read access to this stack's role metadata, image repository metadata, and ECS service deployments and revisions to resolve resource attributes and wait for completion. These permissions do not allow reading provider secret values. It cannot deliver template/IAM/schema changes. An operator reviews and applies those separately with admin credentials and an explicit temporary stack-policy override. Preserve out-of-band SSM/secret values and service identity. Do not replace the bootstrap template with a new stack for an ordinary release.

Rollback uses **Run workflow** on `main` with `image_tag` set to a previous full commit SHA already present in ECR. It reuses that immutable image, creates a fresh task revision, and runs the same canary and post-deploy checks. Do not check out an old workflow or template, change the service name, or delete/recreate the service. ECR has no automatic image expiry, so rollback tags remain available. This rolls back code only; runtime configuration and durable state remain current. SSM and secret rotation also need a new deployment ID, so redeploy the current image after changing their values. Canary rollback keeps reservations for ambiguous paid calls; never clear receipts to make retries work.

## ALB and client address

After every stack deployment attempt, including automatic rollback to a surviving service, `deploy/deploy.py` resolves the ALB from `Gateway.ECSManagedResourceArns.IngressPath.LoadBalancerArn`, sets `idle_timeout.timeout_seconds=300`, `routing.http.xff_header_processing.mode=append`, and disables XFF client ports, then reads the attributes back. It fails on a mismatch and smoke-tests HTTPS. Run the same protected workflow after any out-of-band Express deployment. The November 1 spike proved the default 60-second timeout causes 504s and the 300-second setting permits 120/150/200-second requests; whether Express resets it remains unknown. Do not rely on the previous value surviving a deployment.

The server trusts the final XFF entry only when the direct peer is in `trusted_proxy_cidrs`. In append mode that entry is the client connection observed by ALB, after any caller-supplied prefix. Multiple header lines, IPv6, and malformed suffixes are handled conservatively. The workflow also checks the task's managed security groups: ingress must come only from ALB security groups, with no public/private CIDR or prefix-list ingress. A public task IP must not bypass the ALB. If networking changes, review both this restriction and the CIDRs before serving traffic. There is no preceding CDN/proxy in this design.

## Disable a person and stop paid calls

With a signed-in admin session for this gateway:

```sh
rzm auth admin people
rzm auth admin person <google-sub> --status disabled
rzm auth admin revoke-all <google-sub>
rzm auth admin audit
```

Use the immutable Google subject returned by `people`. A disabled person cannot sign in or refresh and loses new paid calls within 60 seconds. Workspace suspension alone is insufficient. Include this step in offboarding. A re-enable is explicit `--status active`.

The kill switch blocks new paid admissions within 15 seconds:

```sh
rzm auth admin config --paid-calls off
rzm auth admin config
rzm auth admin audit
```

Already dispatched provider work may still be charged. Re-enable with `--paid-calls on` only after investigating. If admin sign-in is unavailable during an incident, an AWS operator can block ALB ingress or stop the service through an incident change. Do not delete the service, table, or secrets to stop spending. SSM edits do not change the seeded runtime switch.

## Logs and failures

Use the `LogGroup` output:

```sh
aws logs tail <log-group> --region <region> --since 1h --follow
```

CloudWatch retains records for 30 days. The gateway writes bounded JSON lines to stdout. `teamapi.request` covers every request, including health and rejected/unknown routes, using registered route patterns rather than URL paths or query strings. Paid-call and auth events add detailed phase timings. Logs contain request ID, authenticated Google subject/session ID, model, counts, sizes, status, duration, and validated client version. They contain no request/response bodies, tokens, email addresses, query strings, or copied headers. Treat subject/session IDs as operational identity data. No ALB access logging is enabled by this template.

A failed workflow is not evidence of a successful release. Inspect CloudFormation events, ECS deployment history, and task exit reasons. An ECS automatic rollback must not pass the revision check. A failure in the ALB or smoke checks happens after a deployment may already be serving; investigate and redeploy a known image. Check the exact running task revision and endpoint. Review billing, retained receipt growth, DynamoDB throttling, and memory under maximum concurrency during the pilot; right-size only from those measurements. This change adds no pager integration or live alarms.

## Provider budgets and first enablement

Each provider has its own UTC admission-day allowance in integer nanodollars (one dollar is one billion nanodollars). Runtime `provider_daily_nanodollars` values are clamped to startup `max_provider_daily_nanodollars`; absent or zero effective values disable that provider. Existing request, byte, and credit limits also apply. Keep actual production allowances and filled configuration in AWS and private operations records.

Before first currency-accounting deployment, turn paid calls off and verify today's admitted usage is zero and no old paid work remains. Legacy paid requests have no trustworthy dollar attribution; a nonzero legacy day refuses new currency-budgeted admission until a clean UTC day or a separately reviewed reconciliation. First deploy the budget-aware binary with the old startup configuration; missing new fields mean zero provider allowance. Verify the requested successful canary and only the new revision serving. Then add the approved startup currency ceilings, deploy a fresh revision of that same image, and apply provider allowances, compatible shared limits, and provider switches through the admin API. Startup SSM values seed runtime state only on first boot; they do not overwrite an existing runtime record.

The gateway reserves the reviewed maximum possible request charge before dispatch and settles down only from unambiguous billing usage. Currency reservations survive missing/malformed usage, rejections without proof of no billing, interrupted responses, and owner recovery. The reservation includes settled charges; do not add them together. TypeSafe dollar accounting uses input tokens only, while existing shared credits keep their prior accounting semantics. Request bodies and `jev-latest` are preserved. Ops reports expose the currently reviewed rates/context bounds; vendor price/context changes require reviewing these assumptions. These controls cover gateway accounting, not unrelated account use or a guarantee against vendor contract changes.

Usage above a reviewed bound fences the provider until accounting review. The fence survives day changes and process restarts. Raising the allowance does not clear it. Global `fenced` reports current admission status even for historical dates; per-person counters describe their admission day. Keep the provider disabled while diagnosing pricing, context, or usage-format changes. Before rollback to a binary without provider-budget enforcement, switch paid calls off and drain. Restore the compatible startup configuration before starting that binary: old strict config decoders reject the new provider fields. Keep paid calls off on an older binary because it cannot enforce provider allowances.

## Ops skill and reports

Use the repository's `rhizome-ops` skill from Codex or Claude Code for service health, usage, logs, performance, access, and incident guidance. Its shared helper provides read-only reports and explicit runtime settings changes. Settings changes preview by default and require `--apply` to write. Supply the expected AWS account, region, and stack explicitly; it refuses a different account before reading the stack. It discovers the endpoint and resource names from stack outputs.

```sh
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> status
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> usage \
  --start 2026-11-01 --end 2026-11-02
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> usage \
  --user operator@example.com
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> performance --hours 1
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> capacity --hours 1
```

Usage and email-filtered logs require an existing admin sign-in to the discovered production origin. If sign-in is missing, use `./scripts/rzm auth login --server <https-origin>`; the helper never reads tokens directly. Usage covers inclusive UTC dates, up to 31 days. It reports admitted requests, input bytes, reserved and settled credits, and unknown outcomes globally and by person. Reserved credits already include settled credits, so never add them. Credits are accounting units, not a provider bill. Limits shown for an old day are today's configured limits. The API has no historical limit snapshots or per-model breakdown. Provider currency totals and current pricing assumptions are reported separately.

Performance reports distinguish HTTP request events from provider events to avoid counting a call twice. They exclude health checks and report sample counts beside latency percentiles. Provider timings separate validation, admission wait, reservation, upstream work, settlement, and total time. Log reads are bounded to 24 hours and an explicit event cap; `complete: false` means the result is partial. A sparse sample or missing measurement is not zero latency or zero traffic.

AWS already exposes service CPU and memory utilization, ALB response time and error counts, and DynamoDB capacity and throttling metrics. The helper pulls a small diagnostic set; use the documented AWS metric dimensions for deeper queries. Missing datapoints are not proof of zero errors. Container Insights, custom application metrics, ALB access logs, dashboards, alarms, and paging are not configured by this template.

For settings, read the current values or preview a change before applying the exact authorized values:

```sh
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> settings
python3 scripts/team-api/ops.py --account <account> --region <region> --stack <stack> settings-update \
  --paid-calls on --voyage on --typesafe on \
  --voyage-daily-usd <approved-USD> --typesafe-daily-usd <approved-USD>
```

Add `--apply` only for an authorized change. The helper reads back requested fields and checks for its matching audit entry. Runtime daily limits must fit the startup ceilings. To raise a ceiling, review a patch to only the relevant `max_person_limits`, `max_global_limits`, and `max_provider_daily_nanodollars` in the non-secret SSM `ConfigParameter`, preserve its other fields, deploy a fresh task through CI, then apply the runtime limits through the admin API. Do not alter DynamoDB directly or expect SSM seed values to replace current runtime settings. Zero effective runtime limits prevent paid admission. Shared request/byte/credit ceilings constrain later admin updates; lowering them does not replace existing runtime limits or per-person overrides. Provider currency ceilings also clamp the effective runtime allowance. Use `--paid-calls off` to stop new paid admissions. Shared credits remain accounting units rather than dollars; the separate provider allowances use the reviewed currency bounds described above.

Recommended follow-ups are an external HTTPS availability check, alerts for sustained target/ALB errors and resource pressure, notification of failed deployments, and an alert on unknown accounting outcomes. Choose thresholds and a notification owner from pilot measurements before provisioning them. Keep per-user accounting in the admin API rather than adding identity labels to CloudWatch metrics.

## Teardown

Teardown is an intentional operator action, never part of the deployment workflow. It loses the random hostname permanently.

1. Disable paid calls, pause production deployments, and wait for in-flight work to finish. Save the endpoint, service ARN, all role ARNs, ALB ARN, secret/parameter names, table name, and repository name outside Git. Take a table backup and agree on retention of receipts/audit data.
2. Delete the Express service explicitly while its infrastructure, execution, and task roles still exist. Use `aws ecs delete-express-gateway-service --service-arn <arn> --region <region> --monitor-resources` with a recent AWS CLI. Wait for deletion to finish and verify the managed ALB, target groups, and security groups are gone. An `INACTIVE`/not-found response is useful evidence, but also check the managed resources. Keep the infrastructure role until cleanup is complete; deleting it early stranded resources in the spike.
3. Only after service cleanup, delete the CloudFormation stack. `Gateway` and its roles have `Retain`; deletion of a stack alone will not stop this service. The retained roles prevent accidental stack deletion from stranding the retained gateway. After stack deletion, manually remove those retained roles and deregister retained task-definition revisions when no service uses them.
4. Keep or deliberately delete the retained ECR repository, SSM parameter, and Secrets Manager secrets according to retention policy. Disable DynamoDB deletion protection only when an approved data deletion is intended; PITR and `Retain` are safeguards, not permission to erase receipts. Never delete a shared GitHub OIDC provider or other Express service's infrastructure. Revoke GitHub environment deployment access and remove its variables when decommissioning is complete.

## Local verification

No AWS access is needed:

```sh
make team-api-check
make team-api-dynamodb-check
docker build --platform linux/arm64 -f services/team-api/Dockerfile \
  --build-arg VERSION=local-deployment-check -t rhizome-team-api:deployment-check .
python3 services/team-api/deploy/container-check.py
python3 -m unittest discover -s services/team-api/deploy -p '*_test.py'
python3 -m unittest discover -s scripts/team-api -p '*_test.py'
cfn-lint services/team-api/deploy/template.yml
actionlint .github/workflows/team-api-deploy.yml
make check
```

The image uses CGO with musl static linking, so SQLite remains available for local use. Scratch contains only the executable, CA certificates, and empty UID-65532-owned directories. ECS uses an explicit ARM64 task definition with a container named `Main` and one named TCP port, as Express requires. Config supplied through `RHIZOME_TEAM_API_CONFIG` defaults diagnostics to stdout; `--config <file>` still works and defaults to local files. The container check uses a disposable Docker network, DynamoDB Local, synthetic credentials, and production config delivered through the environment. It calls no provider or Google endpoint.

## AWS references

- [ECS service CPU and memory metrics](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/available-metrics.html)
- [ALB metrics, dimensions, and reporting gaps](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-cloudwatch-metrics.html)

- [CloudFormation Express service properties and replacement rules](https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-ecs-expressgatewayservice.html).
- [Express custom task definition requirements and ARM64 support](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/express-service-getting-started.html).
- [Express infrastructure role managed policy](https://docs.aws.amazon.com/aws-managed-policy/latest/reference/AmazonECSInfrastructureRoleforExpressGatewayServices.html).
- [ALB X-Forwarded-For append behavior](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/x-forwarded-headers.html).
- [ECS IAM action and resource support](https://docs.aws.amazon.com/service-authorization/latest/reference/list_ecs.html).
- [ECS deployment history and terminal states](https://docs.aws.amazon.com/cli/latest/reference/ecs/list-service-deployments.html).
