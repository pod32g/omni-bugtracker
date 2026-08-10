package worker

import (
	"testing"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
)

// The same status reached by a person, by an automation rule, or by a merged PR is the
// same fact, and a webhook subscriber has no way to tell which path produced it. These
// two mappings therefore have to agree — and they did not: the worker's copy omitted
// `reopened`, so a regression reopened by automation announced itself as a generic
// status change while the human path called it issue.reopened.
//
// serviceStatusEvent mirrors the switch in service.Issues.Transition. It is duplicated
// rather than imported because that switch is inline in a method; if it ever moves to a
// shared helper, delete this and call it directly.
func serviceStatusEvent(to domain.IssueStatus) string {
	switch to {
	case domain.StatusResolved:
		return events.IssueResolved
	case domain.StatusClosed:
		return events.IssueClosed
	case domain.StatusReopened:
		return events.IssueReopened
	}
	return events.IssueStatusChanged
}

func TestStatusEventAgreesWithTheServicePath(t *testing.T) {
	for _, s := range domain.AllStatuses {
		if got, want := statusEvent(s), serviceStatusEvent(s); got != want {
			t.Errorf("status %q: worker emits %q, the service path emits %q", s, got, want)
		}
	}
}

// A close reached by automation has to be announceable as a close, or every webhook
// subscribed to ["issue.closed"] silently misses it — which is what happened when every
// automation action published issue.updated regardless of what it did.
func TestStatusEventDistinguishesTerminalStates(t *testing.T) {
	for _, tc := range []struct {
		status domain.IssueStatus
		want   string
	}{
		{domain.StatusClosed, events.IssueClosed},
		{domain.StatusResolved, events.IssueResolved},
		{domain.StatusReopened, events.IssueReopened},
		{domain.StatusInProgress, events.IssueStatusChanged},
		{domain.StatusBlocked, events.IssueStatusChanged},
	} {
		if got := statusEvent(tc.status); got != tc.want {
			t.Errorf("statusEvent(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
	if statusEvent(domain.StatusClosed) == events.IssueUpdated {
		t.Error("a rule-driven close still announces itself as a generic update")
	}
}
