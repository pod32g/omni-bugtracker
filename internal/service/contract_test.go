package service

import (
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	apispec "github.com/omni/bugtracker/api"
)

// The contract test.
//
// api/openapi.yaml calls itself the source of truth and is served live at /docs, so it
// is what every integrator reads — but nothing checked it against the router, and the
// two had drifted to the point where a third of the surface was undocumented,
// including all of iterations, custom fields, time tracking, reports and export.
//
// This does not attempt to verify that the documented shapes are *correct* — that
// needs request/response validation and is a bigger job. It checks the one thing that
// can be checked cheaply and that catches the common case: a route exists and the spec
// has never heard of it.
//
// The handlers are built with nil dependencies. NewHTTPHandlers only stores them while
// registering routes, so no database is involved and this runs everywhere.

// undocumented is the drift that existed when this test was written.
//
// It is a shrinking allowlist, not a permanent exemption: the point is that the
// backlog is now visible and cannot grow. Deleting an entry after documenting the
// route is the intended way to use this list, and adding one should take an argument.
var undocumented = map[string]bool{
	"DELETE /fields/{id}":                true,
	"DELETE /issues/{issueKey}/mute":     true,
	"DELETE /issues/{issueKey}/snooze":   true,
	"DELETE /iterations/{id}":            true,
	"DELETE /sla-policies/{id}":          true,
	"DELETE /templates/{id}":             true,
	"DELETE /time-entries/{id}":          true,
	"DELETE /views/{id}":                 true,
	"GET /audit":                         true,
	"GET /issues/{issueKey}/fields":      true,
	"GET /issues/{issueKey}/reactions":   true,
	"GET /issues/{issueKey}/references":  true,
	"GET /issues/{issueKey}/time":        true,
	"GET /iterations/{id}/burndown":      true,
	"GET /limits":                        true,
	"GET /me/notification-prefs":         true,
	"GET /me/notifications":              true,
	"GET /ops":                           true,
	"GET /projects/{key}/fields":         true,
	"GET /projects/{key}/issues/export":  true,
	"GET /projects/{key}/issues/similar": true,
	"GET /projects/{key}/iterations":     true,
	"GET /projects/{key}/sla-policies":   true,
	"GET /projects/{key}/templates":      true,
	"GET /projects/{key}/velocity":       true,
	"GET /projects/{key}/views":          true,
	"GET /releases/{id}/notes":           true,
	"GET /reports":                       true,
	"GET /settings/archive":              true,
	"PATCH /fields/{id}":                 true,
	"PATCH /iterations/{id}":             true,
	"PATCH /sla-policies/{id}":           true,
	"PATCH /templates/{id}":              true,
	"PATCH /views/{id}":                  true,
	"POST /issues/{issueKey}/archive":    true,
	"POST /issues/{issueKey}/mute":       true,
	"POST /issues/{issueKey}/rank":       true,
	"POST /issues/{issueKey}/reactions":  true,
	"POST /issues/{issueKey}/read":       true,
	"POST /issues/{issueKey}/snooze":     true,
	"POST /issues/{issueKey}/time":       true,
	"POST /issues/{issueKey}/unarchive":  true,
	"POST /iterations/{id}/carry-over":   true,
	"POST /me/notifications/read":        true,
	"POST /me/saved-searches/{id}/share": true,
	"POST /projects/{key}/fields":        true,
	"POST /projects/{key}/iterations":    true,
	"POST /projects/{key}/sla-policies":  true,
	"POST /projects/{key}/templates":     true,
	"POST /projects/{key}/views":         true,
	"POST /releases/{id}/notes":          true,
	"PUT /issues/{issueKey}/fields":      true,
	"PUT /issues/{issueKey}/iteration":   true,
	"PUT /me/notification-prefs":         true,
	"PUT /settings/archive":              true,
}

func TestEveryRouteIsInTheSpec(t *testing.T) {
	documented := specOperations(t)

	var missing []string
	for _, r := range routerOperations(t) {
		if documented[r] || undocumented[r] {
			continue
		}
		missing = append(missing, r)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d route(s) the server serves and api/openapi.yaml does not describe.\n"+
			"Document them, or — if that is genuinely not happening now — add them to the\n"+
			"`undocumented` allowlist so at least the drift stops growing:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// The reverse direction: a documented operation with no route behind it is worse than
// an undocumented route, because a client can read it, write code against it, and get
// a 404 at runtime.
func TestEverySpecOperationHasARoute(t *testing.T) {
	served := map[string]bool{}
	for _, r := range routerOperations(t) {
		served[r] = true
	}

	var phantom []string
	for op := range specOperations(t) {
		// Some documented endpoints are real but registered elsewhere, so this router
		// has never heard of them: the probes in httpapi.NewRouter (at the root *and*
		// under /api/v1 — see TestProbesAreServedAtTheDocumentedPath, which is where
		// their reachability is actually asserted), /auth/* on the OIDC BFF, and the
		// inbound integration endpoints via mountInboundIntegrations — those
		// authenticate by HMAC rather than bearer, which is exactly why they are
		// mounted in their own group.
		if strings.HasSuffix(op, " /healthz") ||
			strings.HasSuffix(op, " /readyz") ||
			strings.Contains(op, " /auth/") ||
			strings.HasPrefix(op, "POST /integrations/") {
			continue
		}
		if !served[op] {
			phantom = append(phantom, op)
		}
	}
	sort.Strings(phantom)
	if len(phantom) > 0 {
		t.Errorf("%d documented operation(s) with no route behind them — a client that "+
			"believes the spec gets a 404:\n  %s", len(phantom), strings.Join(phantom, "\n  "))
	}
}

// specOperations reads the embedded spec — the bytes actually served at /openapi.yaml,
// so this cannot pass against a stale file on disk.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(apispec.Spec, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	ops := map[string]bool{}
	for path, item := range doc.Paths {
		for key := range item {
			// `parameters`, `summary` and friends sit alongside the verbs.
			if !isHTTPMethod(key) {
				continue
			}
			ops[strings.ToUpper(key)+" "+path] = true
		}
	}
	if len(ops) == 0 {
		t.Fatal("no operations parsed from the embedded spec")
	}
	return ops
}

func routerOperations(t *testing.T) []string {
	t.Helper()
	h := NewHTTPHandlers(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("NewHTTPHandlers no longer returns a chi router; this test needs another " +
			"way to enumerate routes")
	}
	var out []string
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi reports the root of a sub-router with a trailing slash; the spec never
		// writes one.
		if route != "/" {
			route = strings.TrimSuffix(route, "/")
		}
		out = append(out, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no routes walked")
	}
	return out
}

func isHTTPMethod(s string) bool {
	switch strings.ToLower(s) {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	}
	return false
}
