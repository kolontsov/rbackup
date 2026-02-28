version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
commit := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
date := `date -u +%Y-%m-%d`
ldflags := "-s -w -X main.version=" + version + " -X main.commit=" + commit + " -X main.buildDate=" + date

# Build for current platform
build:
    CGO_ENABLED=0 go build -ldflags '{{ldflags}}' -o rbackup .
    rm -f SHA256SUMS
    if command -v sha256sum >/dev/null 2>&1; then sha256sum rbackup > SHA256SUMS; else shasum -a 256 rbackup > SHA256SUMS; fi

# Build all targets (linux/darwin × amd64/arm64)
build-all:
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '{{ldflags}}' -o rbackup-linux-amd64 .
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags '{{ldflags}}' -o rbackup-linux-arm64 .
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags '{{ldflags}}' -o rbackup-darwin-amd64 .
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags '{{ldflags}}' -o rbackup-darwin-arm64 .
    rm -f SHA256SUMS
    if command -v sha256sum >/dev/null 2>&1; then sha256sum rbackup-linux-amd64 rbackup-linux-arm64 rbackup-darwin-amd64 rbackup-darwin-arm64 > SHA256SUMS; else shasum -a 256 rbackup-linux-amd64 rbackup-linux-arm64 rbackup-darwin-amd64 rbackup-darwin-arm64 > SHA256SUMS; fi

# Install to /usr/local/bin
install:
    sudo install -m 0755 rbackup /usr/local/bin/rbackup

# Run unit tests
test:
    go test ./...

# Run integration tests (requires docker)
test-integration:
    go test -tags integration -v -timeout 120s ./integration/...

# Unit test coverage
coverage-unit:
    go test -coverprofile=coverage.unit.out ./...
    go tool cover -func=coverage.unit.out

# Integration test coverage across all packages exercised by integration tests
coverage-integration:
    go test -tags integration -coverpkg=./... -coverprofile=coverage.integration.out ./integration/...
    go tool cover -func=coverage.integration.out

# Coverage for both unit and integration suites
coverage: coverage-unit coverage-integration

# Run all tests (unit + integration)
test-all: test test-integration

# Regenerate RECOVERY.txt from recovery-txt command
update-recovery-txt: build
    ./rbackup recovery-txt -o RECOVERY.txt

# Run linter
lint:
    go vet ./...

# Clean build artifacts
clean:
    rm -f rbackup rbackup-* SHA256SUMS
