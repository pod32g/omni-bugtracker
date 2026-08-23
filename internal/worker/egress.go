package worker

import (
	"net/url"
	"strings"

	"github.com/omni/bugtracker/internal/egress"
)

// webhookHosts bounds concurrent deliveries per destination host. Two is enough to
// keep a healthy receiver busy while leaving room in the shared queue for everybody
// else.
var webhookHosts = egress.NewHostGate(2)

// hostOf extracts the host for gating. An unparseable URL gets its own bucket rather
// than sharing one with every other malformed entry.
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return raw
}
