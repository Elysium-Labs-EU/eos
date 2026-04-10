.PHONY: help dev build install test lint clean docker-*  test-docker-* release release-local fix

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
VERSION_PKG := codeberg.org/Elysium_Labs/eos/internal/buildinfo
LDFLAGS := -ldflags "-X '$(VERSION_PKG).Version=$(VERSION)' -X '$(VERSION_PKG).GitCommit=$(COMMIT)' -X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)' -w -s"

BINARY_NAME=eos
GOBIN=./bin
INSTALL_PATH=~/.local/bin

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2}' | sort

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

test-coverage: ## Get test coverage
	@echo "Getting test coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint: ## Run all linters
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run --timeout=5m
	
fix: ## Fix go formatting
	golangci-lint fmt
	go tool fieldalignment -fix ./...

ci: test lint ## Run all CI checks locally
	@echo "All CI checks passed!"

docker-local: ## Test with local Docker setup
	@echo "Starting local Docker test environment..."
	@mkdir -p test-files-local/nginx-logs
	@sh -c 'docker compose -f test-files-local/docker-compose.yml up --build'

docker-local-down:  ## Tear down local Docker setup
	docker compose -f test-files-local/docker-compose.yml down

docker-local-logs: ## Tail logs local Docker setup
	docker compose -f test-files-local/docker-compose.yml logs -f

docker-vps: ## Test install.sh in VPS simulator
	@echo "Starting VPS simulator..."
	@echo "Once started, run: make docker-vps-test"
	@sh -c 'docker compose -f test-files-vps/docker-compose.yml up --build -d'

docker-vps-down: ## Tear down VPS simulator
	docker compose -f test-files-vps/docker-compose.yml down

docker-vps-test: ## Run install.sh in VPS simulator
	@echo "Testing install.sh in VPS simulator..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh"

docker-vps-shell: ## Open shell in VPS simulator
	@echo "Opening shell in VPS simulator..."
	docker exec -it vps-test-eos bash

docker-vps-status: ## Check eos service status in VPS simulator
	@echo "Checking eos service status in VPS..."
	docker exec -it vps-test-eos systemctl status eos

docker-vps-logs: ## Follow eos logs in VPS simulator
	@echo "Following eos logs in VPS..."
	docker exec -it vps-test-eos journalctl -u eos -f

test-docker-build: ## Build Linux test Docker image
	docker build -f test-files/Dockerfile.test -t eos-test .

test-docker-linux: test-docker-build ## Run tests in Linux Docker container
	docker run --rm eos-test

test-docker-linux-verbose: test-docker-build ## Run tests in Linux Docker container with verbose output
	docker run --rm eos-test go test ./cmd ./internal/... -race -count=1 -v

test-docker-linux-single: test-docker-build ## Run a single test in Linux Docker container (TEST=TestName)
	docker run --rm eos-test go test ./cmd ./internal/... -race -count=1 -v -run $(TEST)

docker-clean: ## Clean up Docker resources
	@echo "Cleaning up Docker resources..."
	docker compose -f test-files-local/docker-compose.yml down -v 2>/dev/null || true
	docker compose -f test-files-vps/docker-compose.yml down -v 2>/dev/null || true
	docker rmi eos-test 2>/dev/null || true
	rm -rf test-files-local/nginx-logs

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

test-install-local: release-local ## Build and test local binary install in VPS simulator
	@$(MAKE) docker-vps
	@sleep 5
	@echo "Copying binary to VPS..."
	docker cp dist/eos-linux-amd64 vps-test-eos:/usr/local/src/eos-local
	docker exec -it vps-test-eos ls -la /usr/local/src/eos-local
	@echo "Running install.sh..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh -y --local /usr/local/src/eos-local"
	docker exec -it vps-test-eos bash -c 'curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.4/install.sh | bash && \. "$$HOME/.nvm/nvm.sh" && nvm install 24 && corepack enable pnpm'

test-install-remote: ## Test remote install.sh in VPS simulator
	@$(MAKE) docker-vps
	@sleep 5
	@echo "Running install.sh..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh"

clean: ## Remove build artifacts and clean Docker resources
	@echo "Cleaning..."
	rm -rf $(GOBIN) dist/
	@$(MAKE) docker-clean
	go clean
	@echo "Cleaned"