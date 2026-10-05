# Contributing to Rhizome

Rhizome is maintained by Atomic Object. Issues and focused pull requests are welcome. Maintainers choose the roadmap and review contributions as capacity allows; there is no guaranteed support or response time.

Before starting a large change, describe the problem and proposed scope in an issue. For a bug report, include the Rhizome version, operating system, reproduction steps, and expected behavior. Remove credentials, private notes, and client information from examples and logs.

## Development

Install [mise](https://mise.jdx.dev/) and run `mise install` for the versions in `mise.toml`, including Go, Node, the Go linter, and gitleaks. A C compiler is required for SQLite. Run `make build` to build the CLI and web UI. Local embeddings are optional; builds and ordinary tests need no API keys.

Read [engineering policy](docs/engineering/README.md) and the relevant [subsystem guide](docs/reference/subsystems/README.md). Keep changes focused, preserve existing work, and add behavior tests where the change needs them. Run `make check-fast` while editing and `make check` before submitting; see [quality gates](docs/engineering/quality-gates.md) for full verification and generated-template checks. Add an Unreleased changelog entry for user-visible changes.

In your pull request, explain the problem, resulting behavior, and verification. Identify remaining limitations. Include appropriate attribution and license text when adding third-party material; see [third-party notices](THIRD_PARTY_LICENSES.md).

Contributions are offered under the project's MIT license in `LICENSE`, except identified third-party material under its own license. Follow the [code of conduct](CODE_OF_CONDUCT.md). Report vulnerabilities through the [security policy](SECURITY.md).
