.PHONY: help dev build install test verify-mod test-linux test-linux-single test-openrc-orb test-install-orb test-fixtures-orb test-integration test-supervision-orb test-launchd lint nilcheck typos crap crap-gate-test leak-test clean release release-local fix setup sg sg-test sg-rules secrets govulncheck check-diff-size check-diff-size-test check-plugin-api-diff check-plugin-api-diff-test bench-mem bench-cpu bench-pprof-mem bench-pprof-cpu bench-diff bench-db bench-db-orb profile-orb adr-find

.DEFAULT_GOAL := help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BENCHMARKS_DIR := __benchmarks__
ORB_MACHINE ?= debian
ORB_IP = $(shell orb ip -m $(ORB_MACHINE) 2>/dev/null)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
VERSION_PKG := $(shell go list -m)/internal/buildinfo
LDFLAGS := -ldflags "-X '$(VERSION_PKG).Version=$(VERSION)' -X '$(VERSION_PKG).GitCommit=$(COMMIT)' -X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)' -w -s"

BINARY_NAME=eos
GOBIN=./bin
INSTALL_PATH=~/.local/bin

PKG ?= ./internal/...

bench-mem: ## Run memory benchmarks on OrbStack $(ORB_MACHINE), save snapshot (all packages)
	@mkdir -p $(BENCHMARKS_DIR)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test -bench=. -benchmem -count=5 ./... 2>&1 | tee $(PWD)/$(BENCHMARKS_DIR)/mem.$(COMMIT).txt"
	@echo "Snapshot: $(BENCHMARKS_DIR)/mem.$(COMMIT).txt"

bench-cpu: ## Run CPU benchmarks on OrbStack $(ORB_MACHINE), save snapshot (all packages)
	@mkdir -p $(BENCHMARKS_DIR)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test -bench=. -count=10 ./... 2>&1 | tee $(PWD)/$(BENCHMARKS_DIR)/cpu.$(COMMIT).txt"
	@echo "Snapshot: $(BENCHMARKS_DIR)/cpu.$(COMMIT).txt"

bench-pprof-mem: ## Profile memory for PKG on OrbStack then open pprof UI (PKG=./internal/foo)
	@mkdir -p $(BENCHMARKS_DIR)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test -bench=. -benchmem -count=5 -memprofile=$(PWD)/mem.out $(PKG)"
	go tool pprof -http=":8082" mem.out

bench-pprof-cpu: ## Profile CPU for PKG on OrbStack then open pprof UI (PKG=./internal/foo)
	@mkdir -p $(BENCHMARKS_DIR)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test -bench=. -count=10 -cpuprofile=$(PWD)/cpu.out $(PKG)"
	go tool pprof -http=":8081" cpu.out

bench-diff: ## Compare two latest memory snapshots with benchstat
	@command -v benchstat >/dev/null 2>&1 || { echo "benchstat not found: go install golang.org/x/perf/cmd/benchstat@latest"; exit 1; }
	@files=$$(ls -t $(BENCHMARKS_DIR)/mem.*.txt 2>/dev/null | head -2); \
	if [ $$(echo "$$files" | wc -w) -lt 2 ]; then echo "Need ≥2 snapshots — run bench-mem on two commits"; exit 1; fi; \
	old=$$(echo "$$files" | awk 'NR==2'); new=$$(echo "$$files" | awk 'NR==1'); \
	echo "comparing $$old → $$new"; benchstat $$old $$new

bench-db: ## Run database benchmarks locally (quick iteration, no snapshot)
	go test -bench=. -benchmem -count=3 ./internal/database/...

bench-db-orb: ## Run database benchmarks on OrbStack $(ORB_MACHINE), save snapshot
	@mkdir -p $(BENCHMARKS_DIR)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test -bench=. -benchmem -count=5 ./internal/database/... 2>&1 | tee $(PWD)/$(BENCHMARKS_DIR)/db.$(COMMIT).txt"
	@echo "Snapshot: $(BENCHMARKS_DIR)/db.$(COMMIT).txt"

profile-orb: ## Capture live heap from daemon on OrbStack (start with: EOS_PPROF_ADDR=:6060 eos daemon start)
	go tool pprof -http=":8082" http://$(ORB_IP):6060/debug/pprof/heap

setup: ## Install dev tools (golangci-lint, git-cliff, lefthook, nilaway) and git hooks
	@if command -v golangci-lint >/dev/null 2>&1 && [ "$$(golangci-lint version 2>&1 | grep -o '2\.12\.2')" = "2.12.2" ]; then \
		echo "golangci-lint v2.12.2 already installed, skipping"; \
	else \
		echo "Installing golangci-lint v2.12.2..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2; \
	fi
	@command -v git-cliff >/dev/null 2>&1 && echo "git-cliff already installed, skipping" || { \
		echo "Installing git-cliff..."; \
		cargo install git-cliff 2>/dev/null || echo "cargo not found — install git-cliff manually: https://git-cliff.org/docs/installation"; \
	}
	@command -v lefthook >/dev/null 2>&1 && echo "lefthook already installed, skipping" || { \
		echo "Installing lefthook..."; \
		go install github.com/evilmartians/lefthook@latest; \
	}
	@command -v nilaway >/dev/null 2>&1 && echo "nilaway already installed, skipping" || { \
		echo "Installing nilaway (nil pointer static analysis)..."; \
		go install go.uber.org/nilaway/cmd/nilaway@latest; \
	}
	@command -v go-crap >/dev/null 2>&1 && echo "go-crap already installed, skipping" || { \
		echo "Installing go-crap (change-risk analysis)..."; \
		go install github.com/padiazg/go-crap@latest; \
	}
	@command -v gitleaks >/dev/null 2>&1 && echo "gitleaks already installed, skipping" || { \
		echo "Installing gitleaks (secret scanning)..."; \
		go install github.com/zricethezav/gitleaks/v8@latest; \
	}
	@command -v govulncheck >/dev/null 2>&1 && echo "govulncheck already installed, skipping" || { \
		echo "Installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	}
	@command -v apidiff >/dev/null 2>&1 && echo "apidiff already installed, skipping" || { \
		echo "Installing apidiff (plugin API breaking-change detection)..."; \
		go install golang.org/x/exp/cmd/apidiff@latest; \
	}
	@command -v benchstat >/dev/null 2>&1 && echo "benchstat already installed, skipping" || { \
		echo "Installing benchstat (benchmark comparison)..."; \
		go install golang.org/x/perf/cmd/benchstat@latest; \
	}
	@command -v typos >/dev/null 2>&1 && echo "typos already installed, skipping" || { \
		echo "Installing typos (spelling check, mirrors the CI typos job)..."; \
		cargo install typos-cli 2>/dev/null || echo "cargo not found; install typos manually: https://github.com/crate-ci/typos#install"; \
	}
	@echo "Installing git hooks..."
	lefthook install
	@echo "Setup complete."

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2}' | sort

list: help ## List all available commands

dev: ## Run eos locally
	@echo "Running eos in development mode..."
	go run . daemon

build: ## Build binary with version info
	@echo "Building eos $(VERSION)..."
	@mkdir -p $(GOBIN)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(GOBIN)/$(BINARY_NAME) .
	@echo "Binary built: $(GOBIN)/$(BINARY_NAME)"

install: build ## Install to ~/.local/bin
	@echo "Installing to $(INSTALL_PATH)..."
	@mkdir -p $(INSTALL_PATH)
	cp $(GOBIN)/$(BINARY_NAME) $(INSTALL_PATH)/
	@echo "Installed! Run 'eos --help' to get started"


test: ## Run tests
	@echo "Running tests..."
	go test ./cmd ./internal/... -race -count=2

verify-mod: ## Verify on-disk module cache matches go.sum (defense-in-depth beyond go.sum presence)
	@echo "Verifying module cache..."
	go mod verify

test-integration: ## Compile+run integration-tagged tests IN PLACE (does not enter a VM despite the OrbStack line it prints; on macOS or without root/systemd, systemd-dependent cases just skip themselves; see test-supervision-orb for real coverage)
	@echo "Running integration tests..."
	@echo "  On OrbStack: orb run -m <machine> -- sudo go test ./cmd/... -tags integration -v -count=1"
	go test ./cmd/... -tags integration -v -count=1

test-launchd: ## Run launchd install/start/stop/uninstall integration tests (native macOS, no orb needed — launchd is macOS-only)
	@echo "Running launchd integration tests..."
	go test ./cmd/... -tags integration -v -count=1 -run 'Launchd'

test-coverage: ## Get test coverage
	@echo "Getting test coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

COVERAGE_THRESHOLD ?= 49

test-coverage-check: ## Fail if total coverage is below COVERAGE_THRESHOLD (default 49%)
	@echo "Checking test coverage (threshold: $(COVERAGE_THRESHOLD)%)..."
	@go test -coverprofile=coverage.out ./... -covermode=atomic -count=1 2>&1 | grep -v "^?" || true
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/{gsub(/%/,""); print $$3}'); \
	echo "Total coverage: $${total}%"; \
	awk -v total="$${total}" -v threshold="$(COVERAGE_THRESHOLD)" \
		'BEGIN { if (total+0 < threshold+0) { print "Coverage " total "% below threshold " threshold "%"; exit 1 } }'
	@echo "Coverage check passed."

lint: ## Run all linters
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run --timeout=5m

nilcheck: ## Static nil-pointer safety analysis (requires: go install go.uber.org/nilaway/cmd/nilaway@latest)
	@echo "Running nilaway nil pointer analysis..."
	@command -v nilaway >/dev/null 2>&1 || { echo "nilaway not found. Run: make setup"; exit 1; }
	nilaway ./...

typos: ## Check for misspellings (mirrors the CI typos job, requires: cargo install typos-cli, see https://github.com/crate-ci/typos#install)
	@echo "Checking for typos..."
	@command -v typos >/dev/null 2>&1 || { echo "typos not found. Run: make setup"; exit 1; }
	typos

GO_CRAP_GATE_PATHS := scripts/go-crap-gate.sh scripts/go-crap-gate_test.sh

crap: test-coverage-check ## Run go-crap change-risk analysis (hard gate on changed functions only, requires: go install github.com/padiazg/go-crap@latest)
	@echo "Running go-crap change-risk analysis..."
	@command -v go-crap >/dev/null 2>&1 || { echo "go-crap not found. Run: go install github.com/padiazg/go-crap@latest"; exit 1; }
	@base="$${GO_CRAP_BASE:-origin/main}"; \
	if ! git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  git fetch --quiet origin "$${base#origin/}" 2>/dev/null || true; \
	fi; \
	changed=""; \
	if git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  diff_base="$$(git merge-base "$$base" HEAD 2>/dev/null || echo "$$base")"; \
	  changed="$$(git diff --name-only "$$diff_base" HEAD -- $(GO_CRAP_GATE_PATHS))$$(git status --porcelain -- $(GO_CRAP_GATE_PATHS))"; \
	else \
	  changed="unknown-base"; \
	fi; \
	if [ -n "$$changed" ]; then \
	  echo "go-crap-gate.sh or its test changed vs $$base (or base unresolvable) -- running crap-gate-test..."; \
	  $(MAKE) crap-gate-test; \
	else \
	  echo "go-crap-gate.sh/_test.sh unchanged vs $$base -- skipping crap-gate-test (run 'make crap-gate-test' to force it)."; \
	fi
	bash scripts/go-crap-gate.sh .

crap-gate-test: ## Real end-to-end test of scripts/go-crap-gate.sh's OS-integration exemption + threshold logic (synthetic reverted commits, requires go-crap)
	@echo "Running go-crap-gate.sh end-to-end self-test..."
	@command -v go-crap >/dev/null 2>&1 || { echo "go-crap not found. Run: go install github.com/padiazg/go-crap@latest"; exit 1; }
	bash scripts/go-crap-gate_test.sh

CHECK_DIFF_SIZE_GATE_PATHS := scripts/check-diff-size.sh scripts/check-diff-size_test.sh

check-diff-size: ## Block oversized files / accidental Git-LFS pointers in the diff vs origin/main (or GO_CRAP_BASE-style CHECK_DIFF_SIZE_BASE)
	@echo "Checking diff for oversized files / LFS pointers..."
	@base="$${CHECK_DIFF_SIZE_BASE:-origin/main}"; \
	if ! git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  git fetch --quiet origin "$${base#origin/}" 2>/dev/null || true; \
	fi; \
	changed=""; \
	if git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  diff_base="$$(git merge-base "$$base" HEAD 2>/dev/null || echo "$$base")"; \
	  changed="$$(git diff --name-only "$$diff_base" HEAD -- $(CHECK_DIFF_SIZE_GATE_PATHS))$$(git status --porcelain -- $(CHECK_DIFF_SIZE_GATE_PATHS))"; \
	else \
	  changed="unknown-base"; \
	fi; \
	if [ -n "$$changed" ]; then \
	  echo "check-diff-size.sh or its test changed vs $$base (or base unresolvable) -- running check-diff-size-test..."; \
	  $(MAKE) check-diff-size-test; \
	else \
	  echo "check-diff-size.sh/_test.sh unchanged vs $$base -- skipping check-diff-size-test (run 'make check-diff-size-test' to force it)."; \
	fi
	bash scripts/check-diff-size.sh

check-diff-size-test: ## Real end-to-end test of scripts/check-diff-size.sh (synthetic reverted commits)
	@echo "Running check-diff-size.sh end-to-end self-test..."
	bash scripts/check-diff-size_test.sh

CHECK_API_DIFF_GATE_PATHS := scripts/check-plugin-api-diff.sh scripts/check-plugin-api-diff_test.sh

check-plugin-api-diff: ## Fail on incompatible changes to the plugin/daemon wire contract (internal/types), requires: go install golang.org/x/exp/cmd/apidiff@latest
	@echo "Checking plugin API surface for breaking changes..."
	@command -v apidiff >/dev/null 2>&1 || { echo "apidiff not found. Run: go install golang.org/x/exp/cmd/apidiff@latest"; exit 1; }
	@base="$${CHECK_API_DIFF_BASE:-origin/main}"; \
	if ! git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  git fetch --quiet origin "$${base#origin/}" 2>/dev/null || true; \
	fi; \
	changed=""; \
	if git rev-parse --verify --quiet "$$base" >/dev/null 2>&1; then \
	  diff_base="$$(git merge-base "$$base" HEAD 2>/dev/null || echo "$$base")"; \
	  changed="$$(git diff --name-only "$$diff_base" HEAD -- $(CHECK_API_DIFF_GATE_PATHS))$$(git status --porcelain -- $(CHECK_API_DIFF_GATE_PATHS))"; \
	else \
	  changed="unknown-base"; \
	fi; \
	if [ -n "$$changed" ]; then \
	  echo "check-plugin-api-diff.sh or its test changed vs $$base (or base unresolvable) -- running check-plugin-api-diff-test..."; \
	  $(MAKE) check-plugin-api-diff-test; \
	else \
	  echo "check-plugin-api-diff.sh/_test.sh unchanged vs $$base -- skipping check-plugin-api-diff-test (run 'make check-plugin-api-diff-test' to force it)."; \
	fi
	bash scripts/check-plugin-api-diff.sh

check-plugin-api-diff-test: ## Real end-to-end test of scripts/check-plugin-api-diff.sh (synthetic reverted commits, requires apidiff)
	@echo "Running check-plugin-api-diff.sh end-to-end self-test..."
	@command -v apidiff >/dev/null 2>&1 || { echo "apidiff not found. Run: go install golang.org/x/exp/cmd/apidiff@latest"; exit 1; }
	bash scripts/check-plugin-api-diff_test.sh

crap-report: ## Full whole-repo go-crap debt report (informational, no gate)
	@command -v go-crap >/dev/null 2>&1 || { echo "go-crap not found. Run: go install github.com/padiazg/go-crap@latest"; exit 1; }
	go-crap scan . --exclude '.*_test\.go'

leak-test: ## Run tests with goroutine leak detection (-count=1, no -race to keep goleak output clean)
	@echo "Running tests with goroutine leak detection..."
	@echo "Note: add 'defer goleak.VerifyNone(t)' or goleak.VerifyTestMain(m) to catch leaks."
	go test ./cmd ./internal/... -count=1 -timeout=60s -v 2>&1 | grep -E "(PASS|FAIL|leak|goroutine)" || true

fix: ## Fix go formatting
	golangci-lint fmt
	go tool fieldalignment -fix ./...

sg: ## Scan codebase with ast-grep rules
	@command -v ast-grep >/dev/null 2>&1 || { echo "ast-grep not found. Install: brew install ast-grep"; exit 1; }
	ast-grep scan

sg-test: ## Run ast-grep rule tests
	@command -v ast-grep >/dev/null 2>&1 || { echo "ast-grep not found. Install: brew install ast-grep"; exit 1; }
	ast-grep test

sg-rules: ## List all ast-grep rules
	@find rules -name '*.yml' | sort

adr-find: ## Find ADRs and related code for a concept: make adr-find Q="daemon liveness"
	@test -n "$(Q)" || { echo "Usage: make adr-find Q=\"concept\""; exit 1; }
	@echo "--- docs/adr matching \"$(Q)\" ---"
	@hits="$$(grep -ril -- "$(Q)" docs/adr/*.md 2>/dev/null)"; \
	if [ -z "$$hits" ]; then \
		echo "  (no filename/content match, try a narrower term)"; \
	else \
		for f in $$hits; do \
			status="$$(grep -m1 '^Status:' "$$f" | sed 's/^Status: *//')"; \
			echo "  $$f  [$${status:-unknown}]"; \
		done; \
	fi
	@echo "--- code comments citing an ADR (ast-grep) ---"
# Both enrichment sections degrade to a notice rather than failing the target:
# ast-grep is an optional host tool, and .gitnexus/ is gitignored so it is absent
# from every git worktree. Exiting non-zero there would withhold the ADR list
# above, which is the part that always works and the reason to run this at all.
	@if command -v ast-grep >/dev/null 2>&1; then \
		ast-grep scan --rule docs/adr/adr-reference.sgrule.yml . 2>/dev/null || echo "  (none found)"; \
	else \
		echo "  (skipped: ast-grep not installed — brew install ast-grep)"; \
	fi
	@echo "--- related code via GitNexus ---"
	@if test -f .gitnexus/run.cjs; then \
		node .gitnexus/run.cjs augment "$(Q)" 2>&1 | { grep -v '^Progress:\|^\[WARN\]\|node_modules\|^Packages:\|^\+\+' || true; }; \
	else \
		echo "  (skipped: no .gitnexus index here — npx gitnexus analyze)"; \
	fi

secrets: ## Scan working tree and history for leaked secrets (requires: go install github.com/zricethezav/gitleaks/v8@latest)
	@command -v gitleaks >/dev/null 2>&1 || { echo "gitleaks not found. Run: make setup"; exit 1; }
	gitleaks detect --source . --no-banner --redact

govulncheck: ## Reachability-aware vulnerability scan (complements OSV-Scanner's lockfile-only scan)
	@command -v govulncheck >/dev/null 2>&1 || { echo "govulncheck not found. Run: make setup"; exit 1; }
	govulncheck ./...

ci: test verify-mod lint sg nilcheck typos test-coverage-check crap check-diff-size check-plugin-api-diff govulncheck secrets ## Run all CI checks locally
	@echo "All CI checks passed!"

ci-full: ci test-linux ## Run make ci plus Linux-parity tests via OrbStack; use before pushing changes to OS-facing packages (procutil, process, manager)
	@echo "All CI checks + Linux parity passed!"

test-linux: ## Run tests on OrbStack $(ORB_MACHINE) Linux (mirrors CI)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test ./cmd ./internal/... -race -count=2"

test-linux-single: ## Run single test on OrbStack $(ORB_MACHINE) (TEST=TestName)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test ./cmd ./internal/... -race -count=1 -v -run $(TEST)"

test-openrc-orb: ORB_MACHINE = alpine
test-openrc-orb: ## Run runtime-detection/OpenRC tests on OrbStack $(ORB_MACHINE) (defaults to an Alpine/OpenRC machine; override with ORB_MACHINE=<name>)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && go test ./cmd/... -race -count=2 -run 'Openrc|OpenRC|DetectActiveSystemRuntime' -v"

# -run 'Supervised' is deliberate, not a placeholder: the full -tags
# integration suite takes ~110s on this VM and currently fails part-way for
# environmental reasons unrelated to the code under test ("error starting
# daemon: timed out waiting for PID file"), while the same suite passes on
# GitHub's ubuntu-latest runner. Scoping to the supervision/daemon-routing
# cases gives a ~4.5s signal that's actually trustworthy to run before every
# push; running the whole suite here would just be permanently red and get
# ignored. Tradeoff: a future supervision test whose name doesn't contain
# "Supervised" silently escapes this target; keep the naming convention.
test-supervision-orb: ## Supervision/daemon-routing integration tests on OrbStack $(ORB_MACHINE) (fast, targeted subset -- see comment above)
	orb run -m $(ORB_MACHINE) bash -lc "export PATH=/usr/local/go/bin:\$$PATH; cd $(PWD) && \
	  sudo env PATH=\$$PATH go test ./cmd/... -tags integration -count=1 -run 'Supervised'"

test-install-orb: release-local ## Build and test install.sh on OrbStack $(ORB_MACHINE) with local binary
	orb run -m $(ORB_MACHINE) bash -lc "arch=\$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'); sudo bash $(PWD)/install.sh -y --local $(PWD)/dist/eos-linux-\$$arch"

test-install-darwin: release-local ## Build and test install.sh natively on macOS with local binary (no orb needed — darwin is native here)
	arch=$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'); sudo bash $(PWD)/install.sh -y --local $(PWD)/dist/eos-darwin-$$arch

SKIP_NODE_INSTALL ?= 0
SKIP_BUN_INSTALL ?= 0
SKIP_PNPM_INSTALL ?= 0
SKIP_JQ_INSTALL ?= 0

test-fixtures-orb: release-local ## Run eos against real nextjs/vite/express/hono fixtures on OrbStack $(ORB_MACHINE) (installs node/bun/pnpm/jq as needed; slow, meant for nightly/pre-release not per-commit). SKIP_{NODE,BUN,PNPM}_INSTALL=1 simulates that interpreter going missing at service-start time instead of skipping install outright; SKIP_JQ_INSTALL=1 tests the script's own precondition failure.
	orb run -m $(ORB_MACHINE) bash -lc "cd $(PWD) && SKIP_NODE_INSTALL=$(SKIP_NODE_INSTALL) SKIP_BUN_INSTALL=$(SKIP_BUN_INSTALL) SKIP_PNPM_INSTALL=$(SKIP_PNPM_INSTALL) SKIP_JQ_INSTALL=$(SKIP_JQ_INSTALL) bash scripts/test-fixtures-orb.sh"

release-local: ## Build release binaries locally
	@echo "Building release binaries..."
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/eos-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/eos-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/eos-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/eos-darwin-arm64 .
	cd dist && sha256sum eos-linux-* eos-darwin-* > sha256sums.txt
	@echo "Release binaries built in ./dist/"
	@ls -lh dist/

changelog: ## Generate CHANGELOG.md from git history
	@echo "Generating CHANGELOG.md..."
	@command -v git-cliff >/dev/null 2>&1 || { echo "git-cliff not found. Install: https://git-cliff.org/docs/installation"; exit 1; }
	git cliff --output CHANGELOG.md
	@echo "CHANGELOG.md updated"

changelog-preview: ## Preview unreleased changes (does not write to file)
	@command -v git-cliff >/dev/null 2>&1 || { echo "git-cliff not found. Install: https://git-cliff.org/docs/installation"; exit 1; }
	git cliff --unreleased

release: ## Update changelog, tag and push a release (requires TAG=v1.2.0)
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.2.0"; exit 1; fi
	@command -v git-cliff >/dev/null 2>&1 || { echo "git-cliff not found. Install: https://git-cliff.org/docs/installation"; exit 1; }
	git cliff --tag $(TAG) --output CHANGELOG.md
	git add CHANGELOG.md
	git diff --cached --quiet CHANGELOG.md || git commit -m "chore: update changelog for $(TAG)"
	git push origin HEAD
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)

pre-release: ## Tag and push a pre-release (requires TAG=v1.2.0-rc.1, no changelog update)
	@if [ -z "$(TAG)" ]; then echo "Usage: make pre-release TAG=v1.2.0-rc.1"; exit 1; fi
	git tag -a $(TAG) -m "Pre-release $(TAG)"
	git push origin $(TAG)

clean: ## Remove build artifacts
	@echo "Cleaning..."
	rm -rf $(GOBIN) dist/
	go clean
	@echo "Cleaned"
