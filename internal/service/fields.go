package service

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// fieldKeyRe mirrors the CHECK on field_definitions.key. Keys are what the filter
// grammar and API clients reference, so they are constrained to something that can
// appear unquoted in a filter string and unescaped in a URL.
var fieldKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,38}$`)

// maxFieldOptions caps a select list. Past this it is not a select, it is a search box,
// and the create form would render an unusable wall of radio buttons.
const maxFieldOptions = 100

func (h *httpHandlers) listFieldDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.repo.ListFieldDefinitions(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if defs == nil {
		defs = []domain.FieldDefinition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": defs})
}

func (h *httpHandlers) createFieldDefinition(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		Key       string   `json:"key"`
		Label     string   `json:"label"`
		Type      string   `json:"type"`
		Options   []string `json:"options"`
		Required  bool     `json:"required"`
		AppliesTo []string `json:"applies_to"`
		HelpText  string   `json:"help_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}

	problems := map[string]string{}
	fieldKey := strings.ToLower(strings.TrimSpace(body.Key))
	if !fieldKeyRe.MatchString(fieldKey) {
		problems["key"] = "lower-case letters, digits and underscores, starting with a letter (max 39)"
	}
	if strings.TrimSpace(body.Label) == "" {
		problems["label"] = "required"
	}
	if !domain.ValidFieldType(body.Type) {
		problems["type"] = "expected text, number, select, multi_select, date, user, checkbox, or url"
	}
	options := dedupeOptions(body.Options)
	if domain.TypeNeedsOptions(body.Type) && len(options) == 0 {
		problems["options"] = "a select needs at least one option — otherwise nobody can fill it in"
	}
	if len(options) > maxFieldOptions {
		problems["options"] = "more than 100 options is a search box, not a select"
	}
	appliesTo, badType := parseIssueTypes(body.AppliesTo)
	if badType != "" {
		problems["applies_to"] = "unknown type " + quoted(badType)
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}

	def, err := h.repo.CreateFieldDefinition(r.Context(), FieldDefinitionInput{
		ProjectKey: key, Key: fieldKey, Label: strings.TrimSpace(body.Label),
		Type: body.Type, Options: options, Required: body.Required,
		AppliesTo: appliesTo, HelpText: body.HelpText,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a field with that key may already exist: "+err.Error())
		return
	}
	h.audit(r, "field.create", "field_definition", def.ID.String(), def.Key, map[string]any{
		"type": def.Type, "required": def.Required,
	})
	writeJSON(w, http.StatusCreated, def)
}

func (h *httpHandlers) updateFieldDefinition(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "field_definition")
	if !ok {
		return
	}
	var body struct {
		Label     *string   `json:"label"`
		Options   *[]string `json:"options"`
		Required  *bool     `json:"required"`
		AppliesTo *[]string `json:"applies_to"`
		HelpText  *string   `json:"help_text"`
		Position  *int      `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	problems := map[string]string{}
	in := UpdateFieldDefinitionInput{
		Label: body.Label, Required: body.Required, HelpText: body.HelpText, Position: body.Position,
	}
	if body.Label != nil && strings.TrimSpace(*body.Label) == "" {
		problems["label"] = "cannot be empty"
	}
	if body.Options != nil {
		options := dedupeOptions(*body.Options)
		if len(options) > maxFieldOptions {
			problems["options"] = "more than 100 options is a search box, not a select"
		}
		in.Options = &options
	}
	if body.AppliesTo != nil {
		types, bad := parseIssueTypes(*body.AppliesTo)
		if bad != "" {
			problems["applies_to"] = "unknown type " + quoted(bad)
		}
		in.AppliesTo = &types
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	def, err := h.repo.UpdateFieldDefinition(r.Context(), id, in)
	if err != nil {
		writeNotFoundOrError(w, err, "field", "update failed")
		return
	}
	h.audit(r, "field.update", "field_definition", def.ID.String(), def.Key, nil)
	writeJSON(w, http.StatusOK, def)
}

func (h *httpHandlers) deleteFieldDefinition(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "field_definition")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteFieldDefinition(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such field")
		return
	}
	h.audit(r, "field.delete", "field_definition", id.String(), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) listIssueFields(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	values, err := h.repo.IssueFieldValues(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if values == nil {
		values = []domain.FieldValue{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

// setIssueFields writes custom field values. Body is a flat object keyed by field key;
// a null or empty value clears the field.
func (h *httpHandlers) setIssueFields(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	defs, err := h.repo.ListFieldDefinitions(r.Context(), issue.ProjectKey)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "load fields failed", err.Error())
		return
	}
	if problems := ValidateFieldValues(defs, issue.Type, body, false); len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	if err := h.repo.SetIssueFieldValues(r.Context(), issue.ID, body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "save failed", err.Error())
		return
	}
	values, err := h.repo.IssueFieldValues(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "reload failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

// ValidateFieldValues checks a batch against the project's definitions, returning
// per-field messages.
//
// `enforceRequired` is off for edits and on at creation time: a field made required
// after an issue was filed must not block every later edit to that issue, or turning a
// field on retroactively locks the backlog.
func ValidateFieldValues(
	defs []domain.FieldDefinition, issueType domain.IssueType, values map[string]any, enforceRequired bool,
) map[string]string {
	problems := map[string]string{}
	byKey := map[string]domain.FieldDefinition{}
	for _, d := range defs {
		byKey[d.Key] = d
	}

	for key, raw := range values {
		def, ok := byKey[key]
		if !ok {
			problems[key] = "no field named " + key + " in this project"
			continue
		}
		if !def.AppliesToType(issueType) {
			problems[key] = def.Label + " does not apply to a " + string(issueType)
			continue
		}
		if msg := validateOneField(def, raw); msg != "" {
			problems[key] = msg
		}
	}
	if enforceRequired {
		for _, def := range defs {
			if !def.Required || !def.AppliesToType(issueType) {
				continue
			}
			if v, ok := values[def.Key]; !ok || isBlank(v) {
				problems[def.Key] = def.Label + " is required"
			}
		}
	}
	return problems
}

func validateOneField(def domain.FieldDefinition, raw any) string {
	if isBlank(raw) {
		return ""
	}
	switch def.Type {
	case domain.FieldSelect:
		s, ok := raw.(string)
		if !ok || !contains(def.Options, s) {
			return "expected one of: " + strings.Join(def.Options, ", ")
		}
	case domain.FieldMultiSelect:
		items, ok := raw.([]any)
		if !ok {
			return "expected a list of values"
		}
		for _, item := range items {
			s, ok := item.(string)
			if !ok || !contains(def.Options, s) {
				return "expected values from: " + strings.Join(def.Options, ", ")
			}
		}
	case domain.FieldNumber:
		if _, ok := raw.(float64); !ok {
			return "expected a number"
		}
	case domain.FieldCheckbox:
		if _, ok := raw.(bool); !ok {
			return "expected true or false"
		}
	case domain.FieldDate:
		s, ok := raw.(string)
		if !ok || !dateOnlyRe.MatchString(s) {
			return `expected a "YYYY-MM-DD" date`
		}
	case domain.FieldUser:
		s, ok := raw.(string)
		if !ok {
			return "expected a user uuid"
		}
		if _, err := uuid.Parse(s); err != nil {
			return "expected a user uuid"
		}
	case domain.FieldURL:
		s, ok := raw.(string)
		if !ok || !(strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			return "expected an http or https URL"
		}
	default:
		if _, ok := raw.(string); !ok {
			return "expected text"
		}
	}
	return ""
}

var dateOnlyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isBlank(raw any) bool {
	switch v := raw.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// dedupeOptions trims, drops blanks, and removes duplicates while keeping the author's
// order — the order is the order the select renders in.
func dedupeOptions(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func parseIssueTypes(in []string) ([]domain.IssueType, string) {
	out := make([]domain.IssueType, 0, len(in))
	for _, s := range in {
		t := domain.IssueType(strings.ToLower(strings.TrimSpace(s)))
		if t == "" {
			continue
		}
		if !domain.ValidType(t) {
			return nil, s
		}
		out = append(out, t)
	}
	return out, ""
}

// resolveFieldFilters turns the parsed `field:` terms into SQL predicates. Unknown keys
// and mistyped values become 422s naming the field, matching what the enum terms do —
// a typo should say what was wrong, not return an empty list or a 500.
func (h *httpHandlers) resolveFieldFilters(r *http.Request, f *IssueFilter) map[string]string {
	if len(f.FieldTerms) == 0 {
		return nil
	}
	problems := map[string]string{}
	for _, term := range f.FieldTerms {
		pred, err := h.repo.ResolveFieldFilter(r.Context(), f.ProjectKey, term[0], term[1])
		if err != nil {
			problems["field"] = err.Error()
			continue
		}
		if pred.Problem != "" {
			problems["field:"+term[0]] = pred.Problem
			continue
		}
		f.FieldPredicates = append(f.FieldPredicates, pred)
	}
	return problems
}
