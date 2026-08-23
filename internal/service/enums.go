package service

import "github.com/omni/bugtracker/internal/domain"

// The four enum-backed issue fields are Postgres enum columns, so an unrecognised
// value is a query error rather than an empty result — pgx reports SQLSTATE 22P02 and
// the handler turns that into a 500. The spec promises 422 for a bad field value, and
// a 500 is worse than cosmetic here: a well-behaved client treats it as transient and
// retries a request that can never succeed.
//
// domain.Valid* already existed and was already called from the filter parser, the SLA
// policy handler and the template handler. These are the write paths that skipped it.

// issueEnumProblems collects a problem entry for each enum field whose value is set
// and unrecognised, keyed by the JSON field name. Nil pointers and empty strings mean
// "not supplied" — absence is the caller's business, not this function's, because
// create requires a type while update treats the same absence as "leave it alone".
func issueEnumProblems(
	typ *domain.IssueType,
	sev *domain.Severity,
	pri *domain.Priority,
	status *domain.IssueStatus,
) map[string]string {
	problems := map[string]string{}
	if typ != nil && *typ != "" && !domain.ValidType(*typ) {
		problems["type"] = "must be one of bug, task, feature, improvement"
	}
	if sev != nil && *sev != "" && !domain.ValidSeverity(*sev) {
		problems["severity"] = "must be one of critical, high, medium, low"
	}
	if pri != nil && *pri != "" && !domain.ValidPriority(*pri) {
		problems["priority"] = "must be one of p0, p1, p2, p3"
	}
	if status != nil && *status != "" && !domain.ValidStatus(*status) {
		problems["status"] = "must be one of open, in_progress, blocked, ready_for_review, " +
			"resolved, closed, reopened"
	}
	return problems
}
