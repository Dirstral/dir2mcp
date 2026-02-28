# Build dir2mcp binary. Requires Go 1.22+.
.PHONY: build
build:
	go build -o dir2mcp ./cmd/dir2mcp/

# Run dir2mcp up (set MISTRAL_API_KEY first)
.PHONY: up
up: build
	./dir2mcp up

# Run tests
.PHONY: test
test:
	go test ./...
