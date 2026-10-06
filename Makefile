BINARY_NAME=rzm
WEB_DIR=web
WEB_DIST=pkg/app/web/assets/dist
WEB_STAMP=$(WEB_DIST)/.web-build
DEV_BINARY=bin/$(GOOS)/$(BINARY_NAME)$(EXE_SUFFIX)

RELEASE_LDFLAGS=-s -w
RELEASE_FLAGS=-trimpath -ldflags "$(RELEASE_LDFLAGS)" -buildvcs=false

# Enable FTS5 for mattn/go-sqlite3
CGO_TAGS=-tags "fts5"

.DEFAULT_GOAL := build

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
NPROC ?= $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
CHECK_JOBS ?= 4
GO_TEST_PACKAGES ?= $(NPROC)
GO_TEST_PARALLEL ?= $(NPROC)
INTEGRATION_JOBS ?= 2

EXE_SUFFIX :=
ifeq ($(GOOS),windows)
EXE_SUFFIX := .exe
endif

ifneq (,$(wildcard local.mk))
include local.mk
endif

build: web-build
	@mkdir -p bin/$(GOOS)
	@mkdir -p .gocache .gomodcache .gotmp
	GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} GOOS=$(GOOS) GOARCH=$(GOARCH) go build -mod=vendor $(CGO_TAGS) -o bin/$(GOOS)/$(BINARY_NAME)$(EXE_SUFFIX)

build-release: web-build
	@mkdir -p bin/$(GOOS)
	@mkdir -p .gocache .gomodcache .gotmp
	GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} GOOS=$(GOOS) GOARCH=$(GOARCH) python3 scripts/release/run_clean.py build -- go build -mod=vendor $(CGO_TAGS) $(RELEASE_FLAGS) -o bin/$(GOOS)/$(BINARY_NAME)$(EXE_SUFFIX)

build-all: web-build
	$(MAKE) build GOOS=darwin GOARCH=amd64
	$(MAKE) build GOOS=linux GOARCH=amd64
	$(MAKE) build GOOS=windows GOARCH=amd64

clean:
	rm -f bin/$(GOOS)/$(BINARY_NAME)$(EXE_SUFFIX)

clean-web:
	rm -f $(WEB_STAMP)

clean-all:
	go clean
	$(MAKE) clean GOOS=darwin GOARCH=amd64
	$(MAKE) clean GOOS=linux GOARCH=amd64
	$(MAKE) clean GOOS=windows GOARCH=amd64

lint:
	@printf '\n==> lint\n'
	@if [ -n "$$(find . -name '*.go' -not -path './vendor/*' -not -path './worktrees/*' -not -path './.worktrees/*' -not -path './.claude/worktrees/*' -not -path './.gocache/*' -not -path './.gomodcache/*' -not -path './.gotmp/*' -exec gofmt -l {} +)" ]; then \
		echo "Run 'go fmt ./...' to fix formatting"; \
		find . -name '*.go' -not -path './vendor/*' -not -path './worktrees/*' -not -path './.worktrees/*' -not -path './.claude/worktrees/*' -not -path './.gocache/*' -not -path './.gomodcache/*' -not -path './.gotmp/*' -exec gofmt -l {} +; \
		exit 1; \
	fi
	@printf '<== lint\n'

CGO_LDFLAGS_ENV :=
ifeq ($(shell uname -s),Darwin)
ifneq ($(RZM_LD_CLASSIC),)
CGO_LDFLAGS_ENV := CGO_LDFLAGS=$${CGO_LDFLAGS:--Wl,-ld_classic}
endif
endif

INTEGRATION_PACKAGES=./tests/integration/...
MIXED_INTEGRATION_PACKAGES=./pkg/vault/cache ./pkg/anchors ./pkg/validate
INTEGRATION_RUN=^TestIntegration

vet:
	@printf '\n==> vet\n'
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go vet -mod=vendor $(CGO_TAGS) ./...
	@printf '<== vet\n'

test:
	@printf '\n==> test\n'
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) RZM_CODE_MODE_RUNTIME_FIXTURE="$$( [ -x '$(DEV_BINARY)' ] && echo '$(PWD)/$(DEV_BINARY)' )" GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go test -mod=vendor -tags "fts5" -race -p $(GO_TEST_PACKAGES) -parallel $(GO_TEST_PARALLEL) ./...
	@printf '<== test\n'

test-fast:
	@printf '\n==> test-fast\n'
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) RZM_CODE_MODE_RUNTIME_FIXTURE="$$( [ -x '$(DEV_BINARY)' ] && echo '$(PWD)/$(DEV_BINARY)' )" GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go test -mod=vendor -tags "fts5" -p $(GO_TEST_PACKAGES) -parallel $(GO_TEST_PARALLEL) ./...
	@printf '<== test-fast\n'

credential-check:
	python3 scripts/secrets/check.py
	python3 -m unittest discover -s scripts/secrets -p '*_test.py'
	python3 -m unittest discover -s scripts/teamkeys -p '*_test.py'

benchmark-test:
	@printf '\n==> benchmark-test\n'
	python3 -m unittest scripts/perf/agent_start_benchmark_test.py
	@printf '<== benchmark-test\n'

integration:
	$(MAKE) -j$(INTEGRATION_JOBS) integration-packages integration-mixed

integration-packages:
	@printf '\n==> integration-packages\n'
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go test -mod=vendor -tags "fts5 integration" -race -p $(GO_TEST_PACKAGES) -parallel $(GO_TEST_PARALLEL) $(INTEGRATION_PACKAGES)
	@printf '<== integration-packages\n'

integration-mixed:
	@printf '\n==> integration-mixed\n'
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go test -mod=vendor -tags "fts5 integration" -race -p $(GO_TEST_PACKAGES) -parallel $(GO_TEST_PARALLEL) -run "$(INTEGRATION_RUN)" $(MIXED_INTEGRATION_PACKAGES)
	@printf '<== integration-mixed\n'

# Three tiers. check-fast is the edit loop, check is the pre-commit gate, and
# check-full adds what CI also runs on every pull request: the race detector,
# integration tests, and benchmark contracts.
CHECK_FAST_TARGETS=lint vet greptile-check web-lint web-typecheck web-typecheck-node

check-fast:
	$(MAKE) web-generate
	$(MAKE) -j$(CHECK_JOBS) $(CHECK_FAST_TARGETS)

check:
	$(MAKE) web-generate
	$(MAKE) -j$(CHECK_JOBS) $(CHECK_FAST_TARGETS) test-fast credential-check
	$(MAKE) web-test

# Each go test target already uses every core, so they run one after another,
# and web-test runs apart from them: its DOM timeouts fail under that load.
check-full:
	$(MAKE) web-generate
	$(MAKE) -j$(CHECK_JOBS) $(CHECK_FAST_TARGETS) test credential-check benchmark-test
	$(MAKE) web-test
	$(MAKE) integration-packages
	$(MAKE) integration-mixed

verify:
	$(MAKE) build
	$(MAKE) check-full
	$(MAKE) web-e2e

check-serial:
	$(MAKE) web-generate
	$(MAKE) lint
	$(MAKE) greptile-check
	$(MAKE) web-lint
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) credential-check
	$(MAKE) benchmark-test
	$(MAKE) integration-packages
	$(MAKE) integration-mixed
	$(MAKE) web-test
	$(MAKE) web-typecheck
	$(MAKE) web-typecheck-node

test_all:
	$(MAKE) -j$(CHECK_JOBS) test benchmark-test integration-packages integration-mixed

test_coverage:
	@mkdir -p .gocache .gomodcache .gotmp
	$(CGO_LDFLAGS_ENV) GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go test -mod=vendor -tags "fts5" -p $(GO_TEST_PACKAGES) -parallel $(GO_TEST_PARALLEL) ./... -coverprofile=coverage.out

greptile-check:
	@printf '\n==> greptile-check\n'
	@tmp=$$(mktemp); \
		if ! scripts/subsystem-guidance-map --greptile > $$tmp; then \
			echo "error: scripts/subsystem-guidance-map --greptile failed"; \
			rm -f $$tmp; \
			exit 1; \
		fi; \
		if ! cmp -s $$tmp .greptile/config.json; then \
			echo "error: .greptile/config.json is stale"; \
			echo "hint: scripts/subsystem-guidance-map --greptile > .greptile/config.json"; \
			rm -f $$tmp; \
			exit 1; \
		fi; \
		rm -f $$tmp
	@printf '<== greptile-check\n'

setup:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		echo "error: not inside a git work tree"; \
		exit 1; \
	fi
	@if ! git ls-files --error-unmatch .codex/config.toml >/dev/null 2>&1; then \
		echo "error: .codex/config.toml is not tracked by git"; \
		echo "hint: commit it first, then re-run 'make setup'"; \
		exit 1; \
	fi
	git update-index --skip-worktree .codex/config.toml
	git config core.hooksPath .githooks
	@echo "OK: local changes to .codex/config.toml will be ignored by git"
	@echo "OK: git hooks will run from .githooks"
	@echo "Undo: make setup-reset"

setup-reset:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		echo "error: not inside a git work tree"; \
		exit 1; \
	fi
	@if git ls-files --error-unmatch .codex/config.toml >/dev/null 2>&1; then \
		git update-index --no-skip-worktree .codex/config.toml; \
	fi
	@if [ "$$(git config --get core.hooksPath || true)" = ".githooks" ]; then \
		git config --unset core.hooksPath; \
	fi
	@echo "OK: .codex/config.toml changes will show up in git status again"
	@echo "OK: repo git hooks disabled"

hooks-setup:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		echo "error: not inside a git work tree"; \
		exit 1; \
	fi
	git config core.hooksPath .githooks
	@echo "OK: git hooks will run from .githooks"
	@echo "Undo: make hooks-reset"

hooks-reset:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		echo "error: not inside a git work tree"; \
		exit 1; \
	fi
	@if [ "$$(git config --get core.hooksPath || true)" = ".githooks" ]; then \
		git config --unset core.hooksPath; \
	fi
	@echo "OK: repo git hooks disabled"

vendor-treesitter:
	./scripts/vendor/update_tree_sitter.sh

build-stripped:
	@mkdir -p .gocache .gomodcache .gotmp
	GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} go build -mod=vendor $(CGO_TAGS) $(RELEASE_FLAGS) -o bin/${BINARY_NAME}

build-small-vault: build-stripped
ifndef VAULT_BIN_DIR
	$(error VAULT_BIN_DIR is not set. Define it in local.mk or your environment)
endif
	install -d $(VAULT_BIN_DIR)
	install -m 0755 bin/${BINARY_NAME} $(VAULT_BIN_DIR)/${BINARY_NAME}
ifdef TEMPLATE_VAULT_BIN_DIR
	install -m 0755 bin/${BINARY_NAME} $(TEMPLATE_VAULT_BIN_DIR)/${BINARY_NAME}
endif

dev-build-backend:
	@mkdir -p bin/$(GOOS)
	@mkdir -p .gocache .gomodcache .gotmp
	GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} GOTMPDIR=$${GOTMPDIR:-$(PWD)/.gotmp} GOOS=$(GOOS) GOARCH=$(GOARCH) go build -mod=vendor $(CGO_TAGS) -o $(DEV_BINARY)

dev-web-prepare:
	cd $(WEB_DIR) && npm install

dev: dev-build-backend dev-web-prepare
	RZM_DEV_REPO_ROOT="$(PWD)" RZM_DEV_BINARY="$(DEV_BINARY)" RZM_DEV_WEB_DIR="$(WEB_DIR)" node scripts/dev_supervisor.mjs

dev-backend: dev-build-backend
	RZM_DEV_REPO_ROOT="$(PWD)" RZM_DEV_BINARY="$(DEV_BINARY)" RZM_DEV_WEB_DIR="$(WEB_DIR)" RZM_DEV_SKIP_VITE=1 node scripts/dev_supervisor.mjs

dev-frontend: dev-web-prepare
	cd $(WEB_DIR) && npm run dev

# Docs: [Release process](docs/RELEASING.md)
.PHONY: build build-release build-all web-build web-generate web-test web-lint web-typecheck web-typecheck-node web-fix web-e2e e2e clean clean-web clean-all lint vet test test-fast credential-check benchmark-test integration integration-packages integration-mixed check check-fast check-full verify check-serial test_all test_coverage greptile-check setup setup-reset hooks-setup hooks-reset vendor-treesitter build-stripped build-small-vault dev-build-backend dev-web-prepare dev dev-backend dev-frontend release release-plan release-dry release-build release-apply release-publish release-resume cut-release release-s3 release-s3-check release-s3-dry
release:
	@if ! git describe --tags --exact-match >/dev/null 2>&1; then \
		echo "error: HEAD is not an exact release tag"; \
		echo "hint: use 'make cut-release' to cut a new release from main"; \
		echo "hint: current commit is $$(git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD)"; \
		exit 1; \
	fi
	GOCACHE=$${GOCACHE:-$(PWD)/.gocache} GOMODCACHE=$${GOMODCACHE:-$(PWD)/.gomodcache} python3 scripts/teamkeys/release.py python3 scripts/release/run_clean.py publish -- goreleaser release --clean

release-plan:
	python3 scripts/release/release_cli.py plan

release-dry:
	python3 scripts/release/release_cli.py dry-run

release-build:
	@test -n "$(RELEASE_BASE)" -a -n "$(RELEASE_HEAD)" || { echo "set RELEASE_BASE and RELEASE_HEAD" >&2; exit 1; }
	python3 scripts/release/release_cli.py build --base "$(RELEASE_BASE)" --head "$(RELEASE_HEAD)"

release-apply:
	@test -n "$(RELEASE_BASE)" -a -n "$(RELEASE_HEAD)" || { echo "set RELEASE_BASE and RELEASE_HEAD" >&2; exit 1; }
	python3 scripts/release/release_cli.py apply --base "$(RELEASE_BASE)" --head "$(RELEASE_HEAD)" $(if $(ACCEPT_DEGRADED_EVIDENCE),--accept-degraded-evidence,)

release-publish:
	@test -n "$(RELEASE_BASE)" -a -n "$(RELEASE_HEAD)" || { echo "set RELEASE_BASE and RELEASE_HEAD" >&2; exit 1; }
	python3 scripts/release/release_cli.py publish --base "$(RELEASE_BASE)" --head "$(RELEASE_HEAD)"

release-resume:
	@test -n "$(RELEASE_BASE)" -a -n "$(RELEASE_HEAD)" || { echo "set RELEASE_BASE and RELEASE_HEAD" >&2; exit 1; }
	python3 scripts/release/release_cli.py resume --base "$(RELEASE_BASE)" --head "$(RELEASE_HEAD)" $(if $(ACCEPT_DEGRADED_EVIDENCE),--accept-degraded-evidence,)

cut-release:
	./scripts/release/cut_release.sh

release-s3:
	@./scripts/release/publish_s3.sh

release-s3-check:
	@PREFLIGHT_ONLY=1 ./scripts/release/publish_s3.sh

release-s3-dry:
	@DRY_RUN=1 ./scripts/release/publish_s3.sh

web-build:
ifndef NO_WEB
	@mkdir -p $(WEB_DIST)
	cd $(WEB_DIR) && npm install
	python3 scripts/licenses/generate.py --check
	cd $(WEB_DIR) && npm run generate:api
	cd $(WEB_DIR) && npm run build
	@touch $(WEB_STAMP)
endif

web-generate:
	@printf '\n==> web-generate\n'
	cd $(WEB_DIR) && npm run generate:api
	@printf '<== web-generate\n'

web-test:
	@printf '\n==> web-test\n'
	cd $(WEB_DIR) && npm test
	@printf '<== web-test\n'

web-lint:
	@printf '\n==> web-lint\n'
	cd $(WEB_DIR) && npm run lint
	@printf '<== web-lint\n'

web-typecheck:
	@printf '\n==> web-typecheck\n'
	cd $(WEB_DIR) && npm run typecheck
	@printf '<== web-typecheck\n'

web-typecheck-node:
	@printf '\n==> web-typecheck-node\n'
	cd $(WEB_DIR) && npm run typecheck:node
	@printf '<== web-typecheck-node\n'

web-fix:
	cd $(WEB_DIR) && npm run lint:fix

web-e2e:
	cd $(WEB_DIR) && npm run test:e2e

e2e: web-e2e

desktop-deps:
	cd desktop && npm ci

desktop-dev:
	cd desktop && npm run tauri -- dev

desktop-build:
	cd desktop && npm run tauri -- build --bundles app

desktop-check:
	cd desktop && npm run check
	cd desktop && npm run bridge
	cd desktop/src-tauri && cargo fmt --check
	cd desktop/src-tauri && cargo test --locked
	cd desktop/src-tauri && cargo clippy --locked --all-targets -- -D warnings

# Build, install, and restart "Rhizome Dev" in /Applications. Main checkout only.
desktop-install-dev:
	desktop/scripts/install-dev.sh

.PHONY: desktop-deps desktop-dev desktop-build desktop-check desktop-install-dev
