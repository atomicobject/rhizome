# Atomic team keys

This package decrypts a release-only AES-256-GCM bundle with `ATOMIC_RHIZOME_KEY`,
resolved from the environment or global Rhizome configuration. Source builds have
an empty bundle. Provider accessors return empty strings when unavailable.

Only Voyage and TypeSafe are bundled. Explicit environment credentials take
precedence. The standalone [TypeSafe client](../typesafe/CONTEXT.md) receives its
credential explicitly. `Available()` means the bundle unlocked; check the specific
accessor when a provider is needed. `ResetCache()` must not race accessor calls.

Never regenerate a tracked bundle. `scripts/teamkeys/release.py` generates one in
a private temporary directory only for internal releases (`RZM_INTERNAL_S3_BUCKET`
set, private GitHub repository), then supplies a Go compiler overlay. This package
is a temporary bridge that may remain in public source; internal releases continue
from the private archive until its replacement is ready. The generator reads only process environment variables and
requires Voyage, TypeSafe, and a 32-byte base64 unlock key. No global-config fallback
is allowed during a release build. OpenAI and Cerebras are not bundled.

Generated bundles carry a marker for portable checks of stripped binaries. The
runtime accepts marked and legacy unmarked bundles without embedding that marker
in ordinary source builds. Public builds require empty tracked bundle source,
reject inherited overlays, disable persisted Go settings, and remove private
mirror settings. GoReleaser checks every built binary and the staged installer
before uploading; internal artifacts require verified private repository visibility.

See [credential policy](../../docs/engineering/secrets.md) for 1Password, releases,
CI, and incident handling. Tests use synthetic encrypted bundles and injected
team-coverage checks, never developer credentials or release unlock keys.
