package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
	"github.com/omni/bugtracker/internal/prose"
)

func (h *httpHandlers) listIssueTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListIssueTemplates(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.IssueTemplate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) createIssueTemplate(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		Name             string   `json:"name"`
		Type             string   `json:"type"`
		BodyMD           string   `json:"body_md"`
		RequiredSections []string `json:"required_sections"`
		DefaultLabels    []string `json:"default_labels"`
		DefaultComponent string   `json:"default_component"`
		DefaultPriority  string   `json:"default_priority"`
		DefaultSeverity  string   `json:"default_severity"`
		IsDefault        bool     `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}

	problems := map[string]string{}
	if strings.TrimSpace(body.Name) == "" {
		problems["name"] = "required"
	}
	issueType := domain.IssueType(strings.ToLower(strings.TrimSpace(body.Type)))
	if !domain.ValidType(issueType) {
		problems["type"] = "expected bug, task, feature, or improvement"
	}
	sections := dedupeOptions(body.RequiredSections)
	// A required section that is not in the template body is a trap: every issue filed
	// from it fails validation on a heading the filer was never shown.
	if len(problems) == 0 {
		if missing := prose.MissingHeadings(body.BodyMD, sections); len(missing) > 0 {
			problems["required_sections"] = "the body has no heading for: " + strings.Join(missing, ", ") +
				" — a required section the filer never sees rejects every issue"
		}
	}
	prio, msg := optionalPriority(body.DefaultPriority)
	if msg != "" {
		problems["default_priority"] = msg
	}
	sev, msg := optionalSeverity(body.DefaultSeverity)
	if msg != "" {
		problems["default_severity"] = msg
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}

	tmpl, err := h.repo.CreateIssueTemplate(r.Context(), IssueTemplateInput{
		ProjectKey: key, Name: strings.TrimSpace(body.Name), Type: issueType,
		BodyMD: body.BodyMD, RequiredSections: sections,
		DefaultLabels: dedupeOptions(body.DefaultLabels), DefaultComponent: body.DefaultComponent,
		DefaultPriority: prio, DefaultSeverity: sev, IsDefault: body.IsDefault,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a template with that name may already exist for this type: "+err.Error())
		return
	}
	h.audit(r, "template.create", "issue_template", tmpl.ID.String(), tmpl.Name, map[string]any{
		"type": string(tmpl.Type), "required_sections": tmpl.RequiredSections,
	})
	writeJSON(w, http.StatusCreated, tmpl)
}

func (h *httpHandlers) updateIssueTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "issue_template")
	if !ok {
		return
	}
	var body struct {
		Name             *string   `json:"name"`
		BodyMD           *string   `json:"body_md"`
		RequiredSections *[]string `json:"required_sections"`
		DefaultLabels    *[]string `json:"default_labels"`
		DefaultComponent *string   `json:"default_component"`
		DefaultPriority  *string   `json:"default_priority"`
		DefaultSeverity  *string   `json:"default_severity"`
		IsDefault        *bool     `json:"is_default"`
		Position         *int      `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	problems := map[string]string{}
	in := UpdateIssueTemplateInput{
		Name: body.Name, BodyMD: body.BodyMD, DefaultComponent: body.DefaultComponent,
		IsDefault: body.IsDefault, Position: body.Position,
	}
	if body.RequiredSections != nil {
		sections := dedupeOptions(*body.RequiredSections)
		in.RequiredSections = &sections
	}
	if body.DefaultLabels != nil {
		labels := dedupeOptions(*body.DefaultLabels)
		in.DefaultLabels = &labels
	}
	if body.DefaultPriority != nil {
		prio, msg := optionalPriority(*body.DefaultPriority)
		if msg != "" {
			problems["default_priority"] = msg
		}
		in.DefaultPriority = prio
	}
	if body.DefaultSeverity != nil {
		sev, msg := optionalSeverity(*body.DefaultSeverity)
		if msg != "" {
			problems["default_severity"] = msg
		}
		in.DefaultSeverity = sev
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	tmpl, err := h.repo.UpdateIssueTemplate(r.Context(), id, in)
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "template", "update failed")
		return
	}
	// Checked against what was actually stored, so a one-field edit cannot leave the
	// body and its required sections disagreeing.
	if missing := prose.MissingHeadings(tmpl.BodyMD, tmpl.RequiredSections); len(missing) > 0 {
		httpapi.WriteValidation(w, map[string]string{
			"required_sections": "the body has no heading for: " + strings.Join(missing, ", "),
		})
		return
	}
	h.audit(r, "template.update", "issue_template", tmpl.ID.String(), tmpl.Name, nil)
	writeJSON(w, http.StatusOK, tmpl)
}

func (h *httpHandlers) deleteIssueTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "issue_template")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteIssueTemplate(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "delete failed", err)
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such template")
		return
	}
	h.audit(r, "template.delete", "issue_template", id.String(), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// optionalPriority / optionalSeverity accept "" as "not set" and validate anything else.
// They return a pointer because the column is nullable and the zero value of the type
// is a real priority.
func optionalPriority(v string) (*string, string) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return nil, ""
	}
	if !domain.ValidPriority(domain.Priority(v)) {
		return nil, "expected p0, p1, p2, p3, or empty"
	}
	return &v, ""
}

func optionalSeverity(v string) (*string, string) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return nil, ""
	}
	if !domain.ValidSeverity(domain.Severity(v)) {
		return nil, "expected critical, high, medium, low, or empty"
	}
	return &v, ""
}

// checklistFor reports an issue body's task-list progress, so the list can show how
// far a definition-of-done has got without opening the issue.
func checklistFor(body string) *domain.ChecklistProgress {
	done, total := prose.Checklist(body)
	if total == 0 {
		return nil
	}
	return &domain.ChecklistProgress{Done: done, Total: total}
}

// applyTemplateDefaults fills in what the filer left blank.
//
// Only blanks: a template that overrode a deliberate choice would be worse than no
// template, because the form would silently disagree with what was submitted.
func applyTemplateDefaults(
	labels, components *[]string, priority *domain.Priority, severity **domain.Severity,
	tmpl domain.IssueTemplate,
) {
	if len(*labels) == 0 && len(tmpl.DefaultLabels) > 0 {
		*labels = tmpl.DefaultLabels
	}
	if len(*components) == 0 && tmpl.DefaultComponent != "" {
		*components = []string{tmpl.DefaultComponent}
	}
	if *priority == "" && tmpl.DefaultPriority != nil {
		*priority = *tmpl.DefaultPriority
	}
	if *severity == nil && tmpl.DefaultSeverity != nil {
		*severity = tmpl.DefaultSeverity
	}
}
