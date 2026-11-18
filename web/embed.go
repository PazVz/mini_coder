// Package web embeds the Vue production build. Run make web-build before Go builds.
package web

import "embed"

// Files contains the SPA entry and Vite's content-hashed assets.
//
//go:embed dist/index.html dist/assets
var Files embed.FS
