.PHONY: help dev build install test lint clean docker-* release release-local fix


help:
	@echo "Available targets:"
	@echo "  make dev              - Run eos locally"
	@echo "  make build            - Build binary with version info"
	@echo "  make install          - Install to ~/.local/bin"
	@echo "  make test             - Run tests"
	@echo "  make lint             - Run all linters"
	@echo "  make ci               - Run all CI checks locally"
	@echo ""
	@echo "Docker testing:"
	@echo "  make docker-local     - Test with local Docker setup"
	@echo "  make docker-vps       - Test install.sh in VPS simulator"
	@echo "  make docker-clean     - Clean up Docker resources"
	@echo ""
	@echo "Release:"
	@echo "  make release-local    	- Build release binaries locally"
	@echo "  make release TAG=v1.2.0  - Tag and push a release"


VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
VERSION_PKG := eos/internal/buildinfo
LDFLAGS := -ldflags "-X '$(VERSION_PKG).Version=$(VERSION)' -X '$(VERSION_PKG).GitCommit=$(COMMIT)' -X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)' -w -s"


BINARY_NAME=eos
GOBIN=./bin
INSTALL_PATH=~/.local/bin


dev:
	@echo "Running eos in development mode..."
	go run . daemon


build:
	@echo "Building eos $(VERSION)..."
	@mkdir -p $(GOBIN)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(GOBIN)/$(BINARY_NAME) .
	@echo "Binary built: $(GOBIN)/$(BINARY_NAME)"

# Install locally
install: build
	@echo "Installing to $(INSTALL_PATH)..."
	@mkdir -p $(INSTALL_PATH)
	cp $(GOBIN)/$(BINARY_NAME) $(INSTALL_PATH)/
	@echo "Installed! Run 'eos --help' to get started"

test:
	@echo "Running tests..."
	go test ./cmd ./internal/... -race -count=2

test-docker-linux:
	@echo "Running tests..."
	docker run --rm -v "$$(pwd)":/app -w /app golang:1.26 go test ./cmd ./internal/... -race -count=1

lint:
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run --timeout=5m
	
fix:
	go tool fieldalignment -fix ./...

ci: test lint
	@echo "All CI checks passed!"

# Docker - Local testing (nginx + eos container)
docker-local:
	@echo "Starting local Docker test environment..."
	@mkdir -p test-files-local/nginx-logs
	@sh -c 'docker compose -f test-files-local/docker-compose.yml up --build'

docker-local-down:
	docker compose -f test-files-local/docker-compose.yml down

docker-local-logs:
	docker compose -f test-files-local/docker-compose.yml logs -f

# Docker - VPS simulator (test install.sh)
docker-vps:
	@echo "Starting VPS simulator..."
	@echo "Once started, run: make docker-vps-test"
	@sh -c 'docker compose -f test-files-vps/docker-compose.yml up --build -d'

docker-vps-down:
	docker compose -f test-files-vps/docker-compose.yml down

docker-vps-test:
	@echo "Testing install.sh in VPS simulator..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh"

docker-vps-shell:
	@echo "Opening shell in VPS simulator..."
	docker exec -it vps-test-eos bash

docker-vps-status:
	@echo "Checking eos service status in VPS..."
	docker exec -it vps-test-eos systemctl status eos

docker-vps-logs:
	@echo "Following eos logs in VPS..."
	docker exec -it vps-test-eos journalctl -u eos -f

# Clean up all Docker resources
docker-clean:
	@echo "Cleaning up Docker resources..."
	docker compose -f test-files-local/docker-compose.yml down -v 2>/dev/null || true
	docker compose -f test-files-vps/docker-compose.yml down -v 2>/dev/null || true
	docker system prune -f
	rm -rf test-files-local/nginx-logs

# Build release binaries locally (for testing before actual release)
release-local:
	@echo "Building release binaries..."
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/eos-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/eos-linux-arm64 .
	cd dist && sha256sum eos-linux-* > sha256sums.txt
	@echo "Release binaries built in ./dist/"
	@ls -lh dist/

release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.2.0"; exit 1; fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)

pre-release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make pre-release TAG=v1.2.0-rc.1"; exit 1; fi
	git tag -a $(TAG) -m "Pre-release $(TAG)"
	git push origin $(TAG)

test-install-local: release-local
	@$(MAKE) docker-vps
	@sleep 5
	@echo "Copying binary to VPS..."
	docker cp dist/eos-linux-amd64 vps-test-eos:/usr/local/src/eos-local
	docker exec -it vps-test-eos ls -la /usr/local/src/eos-local
	@echo "Running install.sh..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh --local /usr/local/src/eos-local"

test-install-remote:
	@$(MAKE) docker-vps
	@sleep 5
	@echo "Running install.sh..."
	docker exec -it vps-test-eos bash -c "cd /test-scripts && bash install.sh"

clean:
	@echo "Cleaning..."
	rm -rf $(GOBIN) dist/
	@$(MAKE) docker-clean
	go clean
	@echo "Cleaned"