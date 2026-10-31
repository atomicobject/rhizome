---
type: ReferenceDoc
summary: "Research and proposed architecture for Google-authenticated team access to Voyage and Jev, with shared desktop sessions and an optional embedding cache."
reference-kind: analysis
last-verified: 2026-10-29
status: active
---

# Shared API access for Atomic Object developers

## Recommendation

Build a small hosted companion service for Rhizome. Its first job should be Google sign-in, individual access control, and forwarding authorized requests to Voyage and TypeSafe/Jev while keeping the provider keys on the server. This is a useful replacement for distributing shared provider credentials. Add shared embedding caching after measuring repeated inputs and initialization latency.

This is a researched proposal, not an approved implementation plan. No cloud resources, Google configuration, credentials, or application behavior were changed during this investigation.

The subsequent [detailed build plan](rhizome-team-api-service-build-plan.md) recommends direct Google desktop OAuth and gateway-managed sessions in place of the Cognito candidate below, and records its scope, security requirements, acceptance checks, and rollout. Both documents remain proposals pending approval; use the build plan for the current proposed implementation sequence.

Use one service identity across desktop and CLI installations, with a session shared per OS user on each computer. Keep the hosted service separate from a vault's local Rhizome runtime. It does not need to host repositories, a knowledge graph, or a remote copy of the local runtime to provide these capabilities.

## What exists today

Source inspection at `dbc04f3a` found these integration points:

- [Credential policy](../../engineering/secrets.md) documents 1Password import into private user configuration and the temporary internal-release key bundle. Removing someone's access to 1Password does not revoke a provider key already copied to their computer. A gateway would make individual revocation possible without rotating a shared upstream key for everyone.
- [The release publisher](../../../scripts/release/publish_s3.sh) uploads artifacts to S3. This establishes release storage, not an existing application server that can authenticate developers or forward inference requests.
- [The Voyage provider](../../../pkg/embeddings/provider_voyage.go) accepts a configured endpoint, requires a static API key, defaults to `voyage-4-lite`, permits 1,000 inputs per batch, and uses a 120-second HTTP timeout. Its lite-model packing budget is about 3.6 MB of input text before JSON encoding. Actual JSON request sizes and vector responses can be larger.
- [The TypeSafe client](../../../pkg/typesafe/client.go) supports a custom base URL and HTTP client. [The current Rhizome caller](../../../pkg/app/mcp/tool_evaluate.go) resolves a provider key and calls the default endpoint. Routing through a gateway therefore needs application integration, not only a server-side change.
- [Local embedding reuse](../../../pkg/embedindex/CONTEXT.md) already exists. [The optional provider cache](../../../pkg/embeddings/provider_helpers.go), selected through `RHIZOME_EMBEDDING_CACHE`, stores vectors in a specified local SQLite file. These do not establish a team-wide cache.
- [Chunk hashes](../../../pkg/embeddings/chunker.go) cover the actual text sent for embedding. [Note preparation](../../../pkg/embedindex/note_chunks.go) adds titles and headings, so identical body text does not necessarily produce identical embedding input in two repositories.

The Rhizome retrieval launcher could not start because this worktree's configured `bin/darwin/rzm` is absent. This assessment used direct source and documentation inspection instead of a live Rhizome retrieval session.

## Sign-in and a session shared across instances

The intended experience is a **Sign in with Atomic Object** action in desktop setup and an equivalent CLI command. The system browser opens Google sign-in. A successful login returns to Rhizome, and later provider requests refresh their access token without another prompt while the session remains valid.

Use Cognito User Pools, on the Essentials tier for refresh-token rotation, federated to Google as the initial authentication candidate. Configure an internal Google OAuth application owned by the Atomic Workspace organization, only the required identity scopes, and the intended Google provider. The app must still enforce eligibility before granting API access. Google Cloud/Workspace setup is a separate permission boundary from AWS administration. [Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect), [Google internal app audience](https://support.google.com/cloud/answer/15544987), [Cognito tiers](https://aws.amazon.com/cognito/pricing/)

Use the stable Google subject as the external identity. Validate issuer, audience, expiration, verified email, and the trusted Workspace `hd` claim against the accepted organization domains. The `hd` authorization parameter is a login hint; an email suffix alone does not prove Workspace membership. With Cognito, establish this check through verified federation claims/admission logic, not client-supplied profile fields. The exact mapping and trigger behavior must be proved in the authentication prototype. [Google token verification](https://developers.google.com/identity/sign-in/web/backend-auth)

For the native client, use authorization code with PKCE through the system browser and a public app client without an embedded client secret. Prefer a hosted HTTPS callback with an expiring, single-use handoff bound to the initiating client's PKCE challenge and redemption secret. The client redeems over HTTPS; reusable tokens never appear in a callback URL. Cognito documents its HTTP loopback callbacks as testing-only, so do not assume Google's production native-app loopback rules also apply to Cognito. The custom handoff needs its own security review. [Native OAuth guidance](https://www.rfc-editor.org/rfc/rfc8252), [Cognito callback rules](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_CreateUserPoolClient.html)

Store the refresh credential in the OS credential store, initially macOS Keychain. All Rhizome processes for that OS user should use one versioned account record and one cross-process refresh lock. A refresh operation reloads the latest credential after acquiring the lock, refreshes only when needed, and saves the replacement before releasing it. This prevents competing instances from invalidating each other's rotated refresh tokens. Keep this record separate from configuration files that older pinned binaries may rewrite. A machine-wide daemon is optional, not a prerequisite. Existing per-vault runtime ownership is the wrong lifetime for this account session.

Keychain sharing must work between the actual signed desktop and CLI releases, not only development processes. Access groups or macOS access-control lists matter; a matching item name alone is insufficient. Verify upgrades, locked Keychain behavior, and a crash between token rotation and local save. Cognito's short rotation grace period can aid recovery but cannot guarantee recovery after an arbitrarily long interruption. [Apple keychain sharing](https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps), [macOS keychain behavior](https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains)

Cognito supports refresh-token rotation, but each rotated token retains the original session's expiration. Its default refresh lifetime is 30 days, configurable between 60 minutes and 10 years. Choose an explicit bounded lifetime, for example 90 days as a policy proposal, and expect occasional browser reauthentication. Do not promise an indefinitely sliding session. [Cognito refresh behavior](https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-the-refresh-token.html)

Give each computer a separately revocable session. Add a server-side account and session-status check to paid API calls, with bounded authorization-cache lifetime. From the documented federation and refresh flows, do not infer that disabling a Google account ends an already issued Cognito session. Even Cognito token revocation does not make a self-contained JWT fail ordinary signature/expiry validation. Define and test the offboarding delay explicitly. Integrate gateway disablement into offboarding or synchronize Workspace eligibility; Google's Cross-Account Protection does not send events for Workspace accounts. Logout should revoke the relevant session, deny its future gateway use, and clear the local credential. [Cognito revocation limits](https://docs.aws.amazon.com/cognito/latest/developerguide/token-revocation.html), [federated authentication flow](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-how-to-authenticate.html), [Google event limitation](https://developers.google.com/identity/protocols/risc)

Direct Google OIDC plus application-owned sessions is a workable alternative. It removes Cognito federation configuration but makes Rhizome responsible for session issuance, rotation, revocation, and signing-key operations. Prefer Cognito unless the prototype shows that its admission or callback constraints add more complexity than they remove.

## Gateway behavior

- Expose fixed, versioned operations for Voyage embeddings and TypeSafe evaluation/model discovery. Do not accept arbitrary upstream URLs or forward arbitrary authenticated requests.
- Validate the access token and required API scope, then check the person's entitlement. Use access tokens for API calls rather than treating an identity token as interchangeable authorization.
- Load upstream keys from AWS Secrets Manager through the service's IAM role. Replace the caller's authorization header with the correct provider key. Keep the two credential types distinct.
- Enforce individual and aggregate concurrency, input-size, model, and spending limits. One person's indexing should not consume the whole shared provider allowance. Budget alerts are useful visibility, not enforcement.
- Preserve upstream response schemas, useful request identifiers, cancellation, and retry guidance. Coordinate retries so client and server do not multiply attempts or replay ambiguous billed Jev requests. Existing [TypeSafe retry behavior](../../../pkg/typesafe/CONTEXT.md) is an integration constraint.
- Record principal, operation, model, counts, estimated/actual usage, duration, and result category. Avoid logging prompts, source text, authorization headers, or raw provider error bodies.
- Keep the service origin and account session in trusted user/application configuration. A repository must not be able to redirect the shared session credential to an arbitrary endpoint. Add refresh-aware transport to both providers rather than putting a short-lived token into the current static API-key setting.
- Retain explicit direct-provider configuration for public users and client work that must use separate provider accounts. Gateway failure should produce a clear retryable or sign-in-required result, not silently switch billing to another account.

## Proposed security baseline

These are recommendations following the security discussion, not deployed controls or an approved configuration. Prioritize account takeover, leaked sessions, unauthorized provider spending, cross-client data exposure, and deployment compromise.

1. **Use Atomic's identity controls.** Require an Atomic-managed Google account and the organization's MFA policy. Prefer passkeys/security keys, especially for service administrators. Restrict access through verified organization membership and a server-controlled eligibility record. Do not add a separate application password.
2. **Make sessions revocable and bounded.** Start with 10-minute access tokens and a 30-day maximum refresh session as proposed defaults. Rotate refresh tokens, store them only in the OS credential store, and coordinate refresh across processes. Support revoking one device or all devices. Check account and session status before paid calls, with a proposed maximum one-minute cache for revocation state. Reject calls when current authorization cannot be established. A short access-token lifetime alone does not solve offboarding.
3. **Keep provider credentials narrowly scoped.** Use dedicated gateway keys for each provider and environment, with provider spending caps where supported. Store them in Secrets Manager. Give the runtime IAM role access only to the exact secrets and tables it needs; it must not have AdministratorAccess. Use standard managed encryption rather than building a key-management system. Retire and rotate previously distributed provider keys after migration. [AWS secret-management guidance](https://docs.aws.amazon.com/secretsmanager/latest/userguide/best-practices.html)
4. **Restrict both destinations and spending.** Implement only the required provider operations and model allowlists. Fix upstream destinations server-side and refuse redirects. Apply request-byte, input-token, execution-time, per-user concurrency, and aggregate limits. Enforce spending limits in the gateway before forwarding work; alerts alone do not stop a stolen session or runaway agent. Include in-flight work when reserving budget.
5. **Keep repository content outside the authentication boundary.** Project configuration and tool input cannot change the trusted gateway origin, read/export account credentials through Rhizome, or select arbitrary authenticated URLs. Send access credentials only to the exact approved HTTPS origin. A malicious process already running as the OS user remains able to attempt legitimate API use; session controls and budgets bound the damage but do not turn an unmanaged laptop into a trusted device.
6. **Minimize retained client data.** Do not persist or log request bodies, source text, provider authorization headers, or raw error bodies. Log the user, operation, usage, status, and timing with a defined short retention period. Keep the first release free of shared content caching. Any later cache needs server-enforced client/project permissions and must be populated only from provider results.
7. **Protect deployment as carefully as login.** Use managed TLS, a maintained runtime, dependency/secret scanning, and reviewed changes to the service and infrastructure. Separate production from development credentials and from release-publishing permissions. Use short-lived CI deployment roles restricted to the intended repository and protected deployment context. A separate AWS account is worthwhile if routine to provision; it is not a prerequisite for the pilot. [GitHub-to-AWS OIDC](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
8. **Prove the failure controls.** Before rollout, test non-member access, expired/wrong-audience tokens, concurrent refresh, revocation of a stolen token, quota exhaustion, and attempts to redirect credentials. Seed synthetic secrets and source text, then inspect logs for leakage. Verify one operator can disable a person, stop all paid calls, and rotate provider keys. Check how outstanding calls are handled when the switch is used.

Initially defer a mandatory VPN, mutual TLS, custom cryptography, per-device hardware attestation, a service mesh, and a custom security dashboard. Add them only for a client requirement or a demonstrated threat that the baseline does not address. Keep routine use to one Google login and automatic renewal; reserve additional interaction for an expired/revoked session or sensitive administration.

## AWS deployment choice

The logical components are Cognito, a small HTTP service, Secrets Manager, minimal authorization/usage state, and CloudWatch. DynamoDB is a reasonable state store; it is also sufficient for an exact embedding cache if that is added. Similarity search can stay in each local Rhizome instance.

I would prototype the HTTP service as a small Go application, then choose hosting using actual request sizes and durations:

1. **A managed container on ECS/Fargate** is the straightforward fit for current synchronous provider behavior and future services. ECS Express Mode can create the service, load balancer, scaling, and networking from a container image and roles. Set the load-balancer timeout deliberately. This has a standing infrastructure cost and needs an actual regional estimate. [ECS Express Mode](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/express-service-create-full.html)
2. **Lambda behind a Regional API Gateway REST API with response streaming** is a viable usage-priced alternative. Current AWS support can stream responses for up to 15 minutes, so the old blanket assertion that API Gateway always imposes a 29-second limit is incorrect. It requires the streaming integration and correct Lambda output format. [REST response streaming](https://docs.aws.amazon.com/apigateway/latest/developerguide/response-transfer-mode.html), [Lambda integration](https://docs.aws.amazon.com/apigateway/latest/developerguide/response-transfer-mode-lambda.html)

Do not use the ordinary API Gateway HTTP API plus buffered Lambda recipe without changing and testing the batching contract. HTTP APIs have a 30-second integration timeout. Lambda synchronous requests and buffered responses have 6 MB limits. Response streaming addresses the response/timeout problem but not the request-size limit; account for JSON escaping and the Lambda event envelope. Rhizome's current 1,000-vector responses and large text batches make these real compatibility checks. [HTTP API quotas](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-quotas.html), [Lambda quotas](https://docs.aws.amazon.com/lambda/latest/dg/gettingstarted-limits.html)

Do not start a new dependency on App Runner: AWS closed it to new customers in April 2026 and directs new container workloads toward ECS Express Mode. [App Runner availability](https://aws.amazon.com/apprunner/)

AWS also now documents **Lambda Web Functions**, a distinct managed HTTP-server product with request timeouts configurable up to 900 seconds. It supports application-managed authorization and streaming, so ordinary Lambda's invocation limits should not be copied onto it. It is a promising additional prototype candidate. Its regional availability, pricing, deployment/runtime support, and custom-domain arrangement need checking before selecting it; the endpoint itself does not directly support a custom domain. This investigation did not deploy or benchmark it. [Web Function quotas](https://docs.aws.amazon.com/lambda/latest/dg/gettingstarted-limits.html), [networking](https://docs.aws.amazon.com/lambda/latest/dg/web-functions-networking.html), [authorization](https://docs.aws.amazon.com/lambda/latest/lambda-web/API_UpdateWebFunctionEndpoint.html)

For operating-cost context, Cognito Essentials currently includes 10,000 monthly active users for direct/social sign-in, with built-in Google counted as social sign-in. The allowance is shared at the account/organization level. Secrets Manager's published example rates are $0.40 per secret-month plus $0.05 per 10,000 API calls. These are component prices, not a total hosting estimate; compute, networking, logs, and upstream usage remain workload-dependent. [Cognito pricing](https://aws.amazon.com/cognito/pricing/), [Secrets Manager pricing](https://aws.amazon.com/secrets-manager/pricing/)

Keep this service's roles, secrets, storage, and deploy permissions separate from release publishing, even if the initial deployment shares the release AWS account. End users need Google sign-in, not IAM users or AWS credentials.

## Shared embedding cache

Implement this behind the existing embedding operation, so local Rhizome instances still receive the same vectors and maintain their own indexes.

For ordinary text embeddings, derive each cache key on the server from an authorized sharing scope, provider/model revision, exact input text, input type, output dimensions/type, truncation behavior, and a cache schema version. Include any other result-affecting options when added. Avoid treating a mutable model alias as a permanent semantic version. Voyage distinguishes query/document inputs and configurable dimensions and encodings. [Voyage embedding API](https://docs.voyageai.com/reference/embeddings-api)

Read hits in batches, send only misses upstream, restore the caller's original order, and populate the cache from provider responses. Deduplicate simultaneous misses so two teammates starting together do not both pay for the same work. Only the server should populate trusted cache entries from provider results; accepting client-uploaded vectors permits cache poisoning. Contextualized chunk models need a different identity that covers the complete context, not just the individual chunk.

The gateway adds an AO-hosted transit path for client text even before caching. For the cache, persist vectors and the minimum identifying metadata rather than source text. This still retains derived client data. Authorize the sharing scope server-side before cache lookup, with client/project boundaries where required. A client-supplied namespace or an Atomic email is not, by itself, permission to share all client-derived embeddings. Avoid an unrestricted hash-lookup endpoint. Global deduplication is appropriate only within an explicitly permitted sharing scope.

The financial benefit needs measurement. Rhizome currently defaults to `voyage-4-lite`, listed at $0.02 per million input tokens after its free allowance. Avoiding 100 million repeated input tokens saves $2 at that list price. Faster first indexing and less rate-limit pressure can matter more than this direct saving. [Voyage pricing](https://docs.voyageai.com/docs/pricing)

Cache storage and requests also cost money, and DynamoDB bills reads in 4 KB units and writes in 1 KB units. A vector store used for exact cache lookup does not need a vector-search index. Measure repeat-hit rate, bytes stored/transferred, upstream tokens avoided, and end-to-end latency before selecting retention and storage. [DynamoDB pricing](https://aws.amazon.com/dynamodb/pricing/)

## Permissions and unresolved evidence

The supplied IAM screenshot shows `AdministratorAccess` attached directly to Drew's IAM user, and no permissions boundary. That is strong evidence of broad AWS setup capability. It does not prove the absence of an AWS Organizations restriction or other explicit deny. [AWS policy evaluation](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html)

A read-only CLI identity check reached AWS after the sandbox retry, but AWS reported that the local session had expired. Effective account permissions, deployed infrastructure, service quotas, DNS ownership, Google Cloud/Workspace permissions, provider account terms, and real traffic were not verified. The account was not modified.

## Smallest useful next experiment

Build an isolated prototype that proves the entire user experience with synthetic payloads:

1. One Google login enables both desktop and CLI in two repositories under the same OS account.
2. Concurrent refreshes, restart, expired sessions, logout, and server-side account disable have defined results. A non-Atomic Google account is rejected.
3. One Voyage-compatible operation and one Jev-compatible operation work through synthetic upstreams, preserving existing response/error behavior. Then run a separately authorized tiny live smoke test.
4. A representative maximum batch and a deliberately slow request succeed through the chosen hosting route. Measure actual serialized bytes and time before fixing deployment limits.
5. Capture usage and duplicate-input counts without retaining content. Decide whether shared caching earns the extra implementation and data-retention work.

Do not make shared caching, remote repository hosting, or a general service platform a prerequisite for this first usable result.
