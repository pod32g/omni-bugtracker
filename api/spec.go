// Package apispec embeds the OpenAPI 3.1 contract (openapi.yaml) so the server can
// serve the raw spec and interactive docs without reading from disk at runtime.
package apispec

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
