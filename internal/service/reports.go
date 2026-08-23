package service

import (
	"net/http"
	"time"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// ReportFilter scopes a report. An empty project key means every project, matching the
// list endpoints.
type ReportFilter struct {
	ProjectKey string
	Since      time.Time
	Until      time.Time
}

// defaultReportDays is the window when none is asked for — long enough for a weekly
// trend to have a shape, short enough to still be about now.
const defaultReportDays = 90

// reports serves the trend view: created vs resolved, backlog age, response and
// resolution percentiles, and throughput.
func (h *httpHandlers) reports(w http.ResponseWriter, r *http.Request) {
	f := ReportFilter{
		ProjectKey: r.URL.Query().Get("project"),
		Until:      time.Now(),
	}
	f.Since = f.Until.AddDate(0, 0, -defaultReportDays)

	for _, spec := range []struct {
		param string
		dest  *time.Time
	}{{"since", &f.Since}, {"until", &f.Until}} {
		raw := r.URL.Query().Get(spec.param)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{spec.param: "expected an RFC 3339 timestamp"})
			return
		}
		*spec.dest = t
	}
	if !f.Since.Before(f.Until) {
		httpapi.WriteValidation(w, map[string]string{"since": "must be before until"})
		return
	}

	rep, err := h.repo.Report(r.Context(), f)
	if err != nil {
		h.serverError(w, r, "report failed", err)
		return
	}
	// Empty slices rather than null, so the client can map over them unconditionally.
	if rep.Flow == nil {
		rep.Flow = []domain.ReportPoint{}
	}
	if rep.Age == nil {
		rep.Age = []domain.ReportAge{}
	}
	if rep.Throughput == nil {
		rep.Throughput = []domain.ReportThroughput{}
	}
	writeJSON(w, http.StatusOK, rep)
}
