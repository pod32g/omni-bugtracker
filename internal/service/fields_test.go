package service

import (
	"testing"

	"github.com/omni/bugtracker/internal/domain"
)

func defs() []domain.FieldDefinition {
	return []domain.FieldDefinition{
		{Key: "tier", Label: "Customer tier", Type: domain.FieldSelect,
			Options: []string{"free", "pro"}, Required: true, AppliesTo: []domain.IssueType{domain.TypeBug}},
		{Key: "score", Label: "Impact score", Type: domain.FieldNumber},
		{Key: "areas", Label: "Areas", Type: domain.FieldMultiSelect, Options: []string{"api", "web"}},
		{Key: "ship_by", Label: "Ship by", Type: domain.FieldDate},
		{Key: "docs", Label: "Docs", Type: domain.FieldURL},
	}
}

func TestValidateFieldValuesTypes(t *testing.T) {
	ok := map[string]any{
		"tier": "pro", "score": 8.0, "areas": []any{"api"},
		"ship_by": "2026-08-01", "docs": "https://example.com/x",
	}
	if problems := ValidateFieldValues(defs(), domain.TypeBug, ok, true); len(problems) != 0 {
		t.Errorf("valid values were rejected: %v", problems)
	}

	bad := map[string]any{
		"tier": "platinum", "score": "eight", "areas": []any{"mobile"},
		"ship_by": "next tuesday", "docs": "example.com",
	}
	problems := ValidateFieldValues(defs(), domain.TypeBug, bad, false)
	for _, key := range []string{"tier", "score", "areas", "ship_by", "docs"} {
		if problems[key] == "" {
			t.Errorf("%s should have been rejected", key)
		}
	}
}

// A required field is enforced at filing time only. Turning one on later must not
// block every subsequent edit to the issues that predate it, or a single settings
// change locks the whole backlog.
func TestValidateFieldValuesRequiredOnlyOnCreate(t *testing.T) {
	empty := map[string]any{}
	if problems := ValidateFieldValues(defs(), domain.TypeBug, empty, true); problems["tier"] == "" {
		t.Error("a required field should be enforced when filing")
	}
	if problems := ValidateFieldValues(defs(), domain.TypeBug, empty, false); len(problems) != 0 {
		t.Errorf("editing should not re-enforce required fields, got %v", problems)
	}
	// A blank string is not an answer to a required field either.
	blank := map[string]any{"tier": "  "}
	if problems := ValidateFieldValues(defs(), domain.TypeBug, blank, true); problems["tier"] == "" {
		t.Error("whitespace should not satisfy a required field")
	}
}

// applies_to is what makes a field project-scoped rather than global: a bug-only field
// must not be required on — or even settable on — a feature request.
func TestValidateFieldValuesAppliesTo(t *testing.T) {
	if problems := ValidateFieldValues(defs(), domain.TypeFeature, map[string]any{}, true); problems["tier"] != "" {
		t.Error("a bug-only field must not be required on a feature")
	}
	problems := ValidateFieldValues(defs(), domain.TypeFeature, map[string]any{"tier": "pro"}, false)
	if problems["tier"] == "" {
		t.Error("setting a bug-only field on a feature should be rejected")
	}
}

func TestValidateFieldValuesUnknownKey(t *testing.T) {
	problems := ValidateFieldValues(defs(), domain.TypeBug, map[string]any{"nope": "x"}, false)
	if problems["nope"] == "" {
		t.Error("an unknown key should be named in the error rather than silently ignored")
	}
}

func TestParseFilterFieldTerms(t *testing.T) {
	f, bad := ParseFilter("BUG", "field:tier:enterprise field:score:8", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected validation errors: %v", bad)
	}
	if len(f.FieldTerms) != 2 {
		t.Fatalf("expected two field terms, got %+v", f.FieldTerms)
	}
	if f.FieldTerms[0] != [2]string{"tier", "enterprise"} {
		t.Errorf("first term = %v", f.FieldTerms[0])
	}

	// A value containing a colon must survive — URLs are a field type.
	f, _ = ParseFilter("BUG", "field:docs:https://example.com/x", "")
	if len(f.FieldTerms) != 1 || f.FieldTerms[0][1] != "https://example.com/x" {
		t.Errorf("a colon in the value should survive, got %+v", f.FieldTerms)
	}

	if _, bad := ParseFilter("BUG", "field:tier", ""); bad["field"] == "" {
		t.Error("field: without a value should be rejected")
	}
}

func TestDedupeOptions(t *testing.T) {
	got := dedupeOptions([]string{" pro ", "free", "pro", "", "  ", "enterprise"})
	want := []string{"pro", "free", "enterprise"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option %d = %q, want %q — author order is the render order", i, got[i], want[i])
		}
	}
}
