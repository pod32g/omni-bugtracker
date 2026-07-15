package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeOpenAPISpec(t *testing.T) {
	rec := httptest.NewRecorder()
	serveOpenAPISpec(rec, httptest.NewRequest("GET", "/openapi.yaml", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "openapi:") {
		t.Fatal("body is not an OpenAPI document")
	}
	// The embedded spec must be the current one (includes the rename-key endpoint).
	if !strings.Contains(body, "/projects/{key}/rename-key") {
		t.Fatal("embedded spec is missing the rename-key endpoint")
	}
}

func TestServeDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	serveDocs(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Fatal("docs page is not Swagger UI")
	}
	if !strings.Contains(body, `url: "/openapi.yaml"`) {
		t.Fatal("Swagger UI is not pointed at /openapi.yaml")
	}
}
