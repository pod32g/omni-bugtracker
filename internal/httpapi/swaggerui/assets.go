// Package swaggerui vendors the Swagger UI static assets (CSS + JS bundle) so the
// API can serve fully self-contained interactive docs — no CDN, no external calls.
//
// Vendored from swagger-ui-dist@5.17.14. To update, re-download the two files:
//
//	V=5.x.y
//	curl -sSLo swagger-ui.css        https://unpkg.com/swagger-ui-dist@$V/swagger-ui.css
//	curl -sSLo swagger-ui-bundle.js  https://unpkg.com/swagger-ui-dist@$V/swagger-ui-bundle.js
//
// Only the bundle is needed (it embeds the default preset); the standalone preset
// isn't used because the docs page pins the spec URL and hides the top bar.
package swaggerui

import "embed"

// Assets holds the vendored Swagger UI files, served under /swagger-ui/.
//
//go:embed swagger-ui.css swagger-ui-bundle.js
var Assets embed.FS
