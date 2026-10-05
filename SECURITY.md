# Security

For a suspected vulnerability, use GitHub's **Security → Report a vulnerability** on this repository when private reporting is available. Include affected versions, reproduction steps, and likely impact. Do not include live credentials or private vault content. Do not post exploit details in a public issue; if private reporting is unavailable, open an issue asking maintainers to arrange a private channel without disclosing the vulnerability.

Reports are handled by Atomic Object maintainers on a best-effort basis. There is no guaranteed response time, supported-version window, or security-fix deadline. Fixes ordinarily target the current release.

Rhizome runs locally. Repository executable trust is an explicit decision stored in user configuration; trust only checkouts whose executable contents you accept. HTML notes are active trusted repository content and can access eligible resources in their vault. Keep the application server on loopback unless you deliberately configure and secure another deployment.

Provider keys belong in your environment or private user configuration. Shared provider credentials must be revoked at the provider after exposure; deleting a file, removing a binary, or rewriting history does not revoke a key.
