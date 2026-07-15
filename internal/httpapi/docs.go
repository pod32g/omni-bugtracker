package httpapi

import "net/http"

import apispec "github.com/omni/bugtracker/api"

// serveOpenAPISpec returns the raw OpenAPI 3.1 YAML (the HTTP contract, embedded at
// build time). Served unauthenticated — it's the API's public documentation.
func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(apispec.Spec)
}

// serveDocs renders Swagger UI pointed at /openapi.yaml. The spec declares
// `servers: /api/v1` + a bearer security scheme, so "Try it out" issues live,
// same-origin calls once you Authorize with an `obt_` token. The Swagger UI assets
// are vendored (internal/httpapi/swaggerui) and served under /swagger-ui/ — no CDN,
// so the docs work fully offline / air-gapped.
func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Omni-BugTracker API</title>
  <link rel="stylesheet" href="/swagger-ui/swagger-ui.css">
  <link rel="icon" href="data:,">
  <style>body { margin: 0; background: #fafafa; }</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="/swagger-ui/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/openapi.yaml",
      dom_id: "#swagger-ui",
      deepLinking: true,
      tryItOutEnabled: true,
      persistAuthorization: true,
    });
  </script>
</body>
</html>
`
