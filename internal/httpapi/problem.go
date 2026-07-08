// Package httpapi wires the chi router, middleware, and (after `make generate`) the
// OpenAPI-generated strict handlers. Errors are emitted as RFC 9457 problem+json.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Problem is an RFC 9457 problem detail.
type Problem struct {
	Type   string            `json:"type,omitempty"`
	Title  string            `json:"title"`
	Status int               `json:"status"`
	Detail string            `json:"detail,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

// WriteProblem serializes a problem response.
func WriteProblem(w http.ResponseWriter, status int, title, detail string) {
	writeProblem(w, Problem{Title: title, Status: status, Detail: detail})
}

// WriteValidation serializes a 422 with per-field errors.
func WriteValidation(w http.ResponseWriter, fields map[string]string) {
	writeProblem(w, Problem{
		Title:  "validation failed",
		Status: http.StatusUnprocessableEntity,
		Errors: fields,
	})
}

func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
