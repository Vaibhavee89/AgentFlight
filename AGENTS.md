# Agent Rules

Rules for any agent (human or AI) writing code in this repository.

- **Stdlib first.** Prefer `log/slog`, `net/http`, `database/sql`, and `encoding/json` over third-party equivalents.
- **Allowed dependencies.** Only `golang.org/x/sync/errgroup` and `modernc.org/sqlite` (pure Go, no cgo) may be added beyond the stdlib. Do not introduce other third-party modules without updating this rule.
- **MCP SDK.** `github.com/modelcontextprotocol/go-sdk` is allowed, but only imported from `internal/tools/mcp`. No other package may import it. `make vet` enforces this.
- **Context placement.** `context.Context` is always the first parameter of a function and is never stored in a struct.
- **Error handling.** Wrap errors with `%w` and check them with `errors.Is` / `errors.As`. Never discard an error silently.
- **Interfaces.** Define interfaces at the point of consumption, not alongside the implementation, and keep them small (prefer one or two methods).
- **Tests.** Every package ships with table-driven tests. All tests must pass with `go test -race ./...`.
- **No global state.** No package-level mutable variables; dependencies are passed explicitly.
- **No panics in library code.** Library code returns errors; `panic` is reserved for truly unrecoverable programmer errors, and never crosses a package boundary.
