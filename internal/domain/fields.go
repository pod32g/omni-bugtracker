package domain

import (
	"time"

	"github.com/google/uuid"
)

// Field types. Typed rather than free text so a custom field can be required,
// validated, sorted and range-queried — the three things labels cannot do.
const (
	FieldText        = "text"
	FieldNumber      = "number"
	FieldSelect      = "select"
	FieldMultiSelect = "multi_select"
	FieldDate        = "date"
	FieldUser        = "user"
	FieldCheckbox    = "checkbox"
	FieldURL         = "url"
)

// FieldDefinition is one project's declaration of an extra field.
type FieldDefinition struct {
	ID    uuid.UUID `json:"id"`
	Key   string    `json:"key"`
	Label string    `json:"label"`
	Type  string    `json:"type"`
	// Options are the permitted values for select / multi_select.
	Options  []string `json:"options"`
	Required bool     `json:"required"`
	// AppliesTo restricts the field to certain issue types; empty means all of them.
	AppliesTo []IssueType `json:"applies_to"`
	HelpText  string      `json:"help_text,omitempty"`
	Position  int         `json:"position"`
	CreatedAt time.Time   `json:"created_at"`
}

// AppliesToType reports whether this field should appear for an issue of that type.
func (d FieldDefinition) AppliesToType(t IssueType) bool {
	if len(d.AppliesTo) == 0 {
		return true
	}
	for _, a := range d.AppliesTo {
		if a == t {
			return true
		}
	}
	return false
}

// FieldValue is one field's value on one issue, rendered generically. Value carries
// whichever typed column the definition selects, so clients switch on Type rather
// than on the field's key — no per-project frontend code, ever.
type FieldValue struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	// Value is string, float64, bool, []string, or a user id, per Type. Nil when the
	// field is defined but unset — which is different from an empty string.
	Value any `json:"value"`
	// User is resolved for the "user" type so the client need not look it up.
	User *User `json:"user,omitempty"`
}

// ValidFieldType guards a value headed for the Postgres enum column.
func ValidFieldType(t string) bool {
	switch t {
	case FieldText, FieldNumber, FieldSelect, FieldMultiSelect,
		FieldDate, FieldUser, FieldCheckbox, FieldURL:
		return true
	}
	return false
}

// TypeNeedsOptions reports whether a definition of this type requires an option list.
// A select with no options is a field nobody can fill in.
func TypeNeedsOptions(t string) bool {
	return t == FieldSelect || t == FieldMultiSelect
}
