// Package web embeds the built single-page UI. The assets live in dist/, which
// `make web-build` produces; a committed placeholder keeps go build working on a
// clone that has no Node toolchain.
//
// go:embed can only reach files inside its own package directory, which is why
// the embed lives here rather than in internal/webui. internal/webui serves it.
package web

import "embed"

// FS holds the contents of dist/, with paths relative to dist/.
//
//go:embed dist
var FS embed.FS
