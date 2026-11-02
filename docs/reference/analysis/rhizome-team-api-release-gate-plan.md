---
type: ReferenceDoc
summary: "Approved release-branch deployment gate that verifies the gateway before publishing Rhizome client artifacts."
reference-kind: analysis
status: approved
---

# Gate client releases on a verified gateway deployment

Approved outcome: every supported Rhizome publication path waits for GitHub Actions to deploy and verify the intended server source before GoReleaser or S3 uploads client artifacts. AWS credentials remain in the existing protected production workflow. Release retries retain the original server source selection.

Status: approved by Drew on 2026-11-02 ("Let's do that"). Implementation and deployment-only verification are authorized; cutting or publishing a client release is excluded.

## Decisions and evidence

This changes the release contract across local orchestration and GitHub Actions. Drew supplied the explicit approval required by `docs/engineering/review-and-approval.md`.

A main release currently pushes both canonical branches to the release commit. A release hotfix pushes its release commit to `release` and a synthetic main-first merge to `main`. The existing deployment workflow triggers only selected main paths and builds `GITHUB_SHA`. A version-only release need not trigger it, and a hotfix's main merge SHA is not the client release SHA.

Internal clients still publish from a verified private repository. Its history is separate from the public repository. The retained private archive has separate history and predates the gateway source. An ancestry test across those histories cannot establish matching server source; an internal release must first receive the matching public gateway inputs.

Drew selected the `release` branch as the server source. Resolve its exact commit automatically, record that commit for retry, and require deployment to finish before client publication. `main` holds the trusted workflow; it does not select the server source. There is no manual SHA-selection step.

## Source selection and agreement

For a public `atomicobject/rhizome` release, deploy the exact validated client release commit, including a hotfix commit rather than its main merge. Require it to equal `origin/release` and have the existing canonical integration into main.

For a private internal release, resolve `atomicobject/rhizome` release once to a full 40-character SHA. Persist that choice in the existing release run directory before dispatch. Resume reuses it even if public main moves. Require the selected public commit to have the canonical release integration into public main. Never send a private commit, Git tree, source archive, mirror location or secret to the public workflow.

Before dispatch, compare the private client's committed release snapshot with the selected public server commit for every server Docker build input. Current inputs are:

- `go.mod` and `go.sum`.
- `pkg/diagnostics`, `pkg/fileio`, `pkg/typesafe`, and `pkg/teamapi/wire`.
- Entire `services/team-api`, including its Dockerfile, nested module dependencies and deployment helpers.
- Presence and content of optional root `.dockerignore` and `services/team-api/Dockerfile.dockerignore`.

Build a deterministic fingerprint from each path's Git object mode, type and object ID, with explicit absence markers only for optional ignore files. Compare committed trees, never worktree files. Public Git tree API reads can provide the public object IDs without fetching private history into the public repository. Reject missing required paths, truncated tree responses, lookup errors and mismatches. Report differing paths and hashes, never source content or secret values.

Keep a small explicit build-input manifest and validate the Dockerfile's non-stage `COPY` sources against it at runtime. Support its current simple syntax and fail closed if a new source, glob or unsupported COPY syntax appears. Reject unreviewed Docker parser directives and RUN context mounts rather than silently excluding their inputs. This prevents a future Dockerfile dependency from escaping the agreement check. The entire service directory comparison is conservative: documentation or test differences can block publication. Resolve by synchronizing the relevant public source into the private release checkout, not by overriding the gate.

This proves that the server source accompanying the private client is deployed. It cannot prove an untested client feature works. Existing client/server tests remain required. A private client change that depends on new gateway capabilities must first land those server changes publicly, then synchronize the matching build inputs into the private checkout.

## GitHub Actions changes

Remove its automatic main-push trigger so unreleased main changes cannot deploy between releases. Extend `.github/workflows/team-api-deploy.yml` with separate `source_commit` and `request_id` dispatch inputs. Retain existing manual `image_tag` rollback mode; reject mixed rollback/build inputs. Add an exact run name containing the correlation ID and source SHA.

Dispatch using `--ref main` in `atomicobject/rhizome`, preserving the production environment's main-only rule and current OIDC trust. Checkout main first, validate input shape and trusted public main ancestry, then build and test the selected immutable source commit in a separate checkout. Execute the deployment helper from the trusted dispatcher main checkout, so an older hotfix source cannot discard current deployment verification fixes. Use that source SHA consistently for the image tag, binary version and deploy argument. Public release orchestration performs the stronger release topology check before dispatch. The existing `deploy.py` remains the AWS deployment and verification owner.

Do not grant the private archive AWS access. Its operator uses `gh` authorization to dispatch the public workflow. No provider keys or client release credentials enter the dispatch inputs.

## Local release gate and correlation

Add `scripts/release/release_gateway.py` with injectable GitHub command runner and clock for behavioral tests. Resolve and validate the source as above, write a small local receipt containing target repository, release SHA, server SHA, source fingerprint and a random request ID, then dispatch. Poll only this named workflow's `workflow_dispatch` runs with the exact request ID/run title. Require one match, correct repository, main event ref and expected workflow. Never choose the newest main run or compare dispatcher `headSha` to the separate source SHA.

Persist the matched run ID and URL. Wait with a bounded deadline long enough for the current 150-minute workflow plus approval delay. Print the workflow URL promptly, including when approval is pending. Non-success terminal states, missing/ambiguous runs, API errors and timeouts block client publication and retain the receipt. A dispatch with an uncertain response is reconciled by its saved request ID before retry; do not blindly redispatch.

On resume, reuse pending runs. If the prior run failed, create a fresh attempt for the same saved server SHA. If the prior run succeeded but no client publication happened, run a fresh deployment verification attempt before publishing so a stale success cannot establish current production state. Immutable ECR images and the existing deployment concurrency group make repeat attempts safe. No new release phase or checkpoint is needed. Revalidate canonical client branches again after the deployment wait and immediately before uploads. The existing check occurs before the proposed wait and is insufficient on its own.

## Cover all publishers

- `Orchestrator.publish()` calls the gate after its initial `publication_complete()` and before `publish_command()`, then repeats canonical-topology validation after the gate returns. Gate failure must return the existing precise resume command and leave GitHub/S3 publication untouched.
- An internal resume that only needs S3 still runs the gate before that remaining client upload.
- Direct `make release` must invoke the same gated publication entry point after exact-tag/topology validation; it must not call GoReleaser directly.
- The manual `.github/workflows/release.yml` fallback must depend on successful deployment of its validated release commit before its write-capable GoReleaser job. Prefer calling a reusable version of the existing deployment job, preserving its production protection and shared concurrency. If using dispatch instead, give only the gate job `actions: write` and use the same correlation helper; never add a second deploy implementation.
- `make release-s3` needs the same gate before uploads, while preflight/dry-run modes remain read-only. Pass a verified local receipt from orchestration to avoid duplicate deployment in one uninterrupted publish command; validate its release/source identity and live originating publisher process rather than trusting an arbitrary bypass environment flag. A receipt from an ended invocation must trigger a fresh attempt even if its old GitHub run succeeded.
- Direct invocation of third-party GoReleaser remains outside the supported release interface. Documentation names the supported gated commands.

## Verification and delivery

Add behavioral tests for public main releases, hotfix SHA selection, private automatic release-branch pinning, matching source trees, missing/mismatched build inputs, changed Dockerfile COPY sources, and separate public/private histories. Test agreement checks before any workflow dispatch.

Test correlated run selection against unrelated runs, duplicate matches, timeout, cancellation/failure, uncertain dispatch recovery, pending-run resume and fresh verification after prior success. Test that every gate failure prevents GoReleaser and S3 uploads and preserves resumable state. Test private mode cannot pass secrets/private source in dispatch fields.

Extend release orchestration tests for ordering after atomic branch publication and before the first client upload. Cover S3-only resume and public fallback/direct Makefile paths. Preserve existing tag-conflict and release-mode refusal tests. Run the repository's Python release suites and applicable quality gates; inspect generated workflow YAML with its established validation tooling.

Update `docs/RELEASING.md`, `scripts/release/CONTEXT.md`, the team API runbook and relevant release policy to describe source selection, source agreement failures, production approvals and retry behavior. Keep private mirror references out of tracked files.

A real test requires the changes to land on public main first. Run a deployment-only test of the pinned source through the protected workflow and inspect its successful canary, image identity and health evidence. Do not cut or publish a client release as a test without explicit authorization.
