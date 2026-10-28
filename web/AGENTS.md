# Web UI rules

Rules for any agent (human or AI) changing the frontend in this directory.

- **No new dependency without a stated reason.** Add a package only when the
  reason is written in the commit message. Prefer the platform and the existing
  stack. Current runtime dependencies are react, react-dom, react-router-dom,
  and recharts. Dev dependencies are the build, lint, and test tooling.
- **Pinned versions.** Pin exact versions, not ranges, and keep each release at
  least two weeks old when it is added.
- **API types mirror the Go structs.** Every type in `src/api/types.ts` must
  match the JSON the Go server writes, field for field, name for name. When a
  Go struct changes, change the matching type in the same commit.
- **All fetching goes through `src/api/client.ts`.** Components and hooks must not
  call `fetch` directly. This keeps URLs, error handling, and JSON validation in
  one place.
- **No `any`.** Decode JSON as `unknown` and narrow it. Type assertions are only
  allowed where narrowing is not possible, and must carry a comment saying why.
- **Plain CSS with variables.** No UI framework. Colors and spacing are CSS
  custom properties defined in `src/styles.css` on `:root`.
- **Strict TypeScript.** `tsc --noEmit` must pass with the strict flags in
  `tsconfig.json` unchanged.
- **Every module has tests.** Components and client functions ship with Vitest
  tests under `src/test/`.
