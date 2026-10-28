.PHONY: build test race vet bench web-install web-build web-dev

MCP_SDK := github.com/modelcontextprotocol/go-sdk
MCP_ALLOWED := ./internal/tools/mcp/

build: web-build
	go build ./...

web-install:
	cd web && npm ci

web-build: web-install
	cd web && npm run build

web-dev:
	cd web && npm run dev

test:
	go test ./...

race:
	go test -race ./...

vet:
	@offenders=$$(grep -rl --include='*.go' '"$(MCP_SDK)' . | grep -v '^$(MCP_ALLOWED)'); \
	if [ -n "$$offenders" ]; then \
		echo "error: $(MCP_SDK) may only be imported from $(MCP_ALLOWED):" >&2; \
		echo "$$offenders" >&2; \
		exit 1; \
	fi
	go vet ./...

bench:
	go test -bench=. -run=^$$ ./...
