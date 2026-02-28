# Build dir2mcp binary. Requires Go 1.22+.
.PHONY: build
build:
	go build -o dir2mcp ./cmd/dir2mcp/

# Run dir2mcp up (set MISTRAL_API_KEY first)
.PHONY: up
up: build
	./dir2mcp up

.PHONY: help fmt vet lint test check ci

help:
	@echo "Targets:"
	@echo "  fmt    - format Go code"
	@echo "  vet    - run go vet"
	@echo "  lint   - run golangci-lint"
	@echo "  test   - run go test"
	@echo "  check  - fmt + vet + lint + test"
	@echo "  ci     - vet + test (CI-safe default)"

fmt:
	gofmt -w $$(find cmd internal tests -name '*.go')

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required. Install: https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run

test:
	go test ./...

check: fmt vet lint test

ci: vet test
