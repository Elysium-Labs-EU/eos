.PHONY: help dev build install test test-linux test-linux-single test-openrc-orb test-install-orb test-integration test-launchd lint nilcheck leak-test clean release release-local fix setup sg sg-test sg-rules bench-mem bench-cpu bench-pprof-mem bench-pprof-cpu bench-diff bench-db bench-db-orb profile-orb

.DEFAULT_GOAL := help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BENCHMARKS_DIR := __benchmarks__
ORB_MACHINE ?= debian
ORB_IP = $(shell orb ip -m $(ORB_MACHINE) 2>/dev/null)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
VERSION_PKG := codeberg.org/Elysium_Labs/eos/internal/buildinfo
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
	@echo "Installing golangci-lint v2.11.0..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v2.11.0
	@echo "Installing git-cliff..."
	cargo install git-cliff 2>/dev/null || echo "cargo not found — install git-cliff manually: https://git-cliff.org/docs/installation"
	@echo "Installing lefthook..."
	go install github.com/evilmartians/lefthook@latest
	@echo "Installing nilaway (nil pointer static analysis)..."
	go install go.uber.org/nilaway/cmd/nilaway@latest
	@echo "Installing benchstat (benchmark comparison)..."
	go install golang.org/x/perf/cmd/benchstat@latest
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

test-integration: ## Run integration tests (requires Linux + systemd + root; use OrbStack)
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
	@find rules -name '*.yml' ! -path '*__tests__*' | sort

ci: test lint sg nilcheck test-coverage-check ## Run all CI checks locally
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

test-install-orb: release-local ## Build and test install.sh on OrbStack $(ORB_MACHINE) with local binary
	orb run -m $(ORB_MACHINE) bash -lc "arch=\$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'); sudo bash $(PWD)/install.sh -y --local $(PWD)/dist/eos-linux-\$$arch"

release-local: ## Build release binaries locally
	@echo "Building release binaries..."
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/eos-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/eos-linux-arm64 .
	cd dist && sha256sum eos-linux-* > sha256sums.txt
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
