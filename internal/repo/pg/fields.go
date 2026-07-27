package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

const selectFieldDefinition = `
	SELECT d.id, d.key, d.label, d.type::text, d.options, d.required,
	       d.applies_to::text[], d.help_text, d.position, d.created_at
	  FROM field_definitions d JOIN projects p ON p.id = d.project_id`

func scanFieldDefinition(row scanner) (domain.FieldDefinition, error) {
	var d domain.FieldDefinition
	var appliesTo []string
	if err := row.Scan(&d.ID, &d.Key, &d.Label, &d.Type, &d.Options, &d.Required,
		&appliesTo, &d.HelpText, &d.Position, &d.CreatedAt); err != nil {
		return domain.FieldDefinition{}, err
	}
	// Both slices are initialised so the JSON carries [] rather than null — a client
	// that has to handle both is one that will eventually handle only one.
	d.AppliesTo = make([]domain.IssueType, 0, len(appliesTo))
	for _, t := range appliesTo {
		d.AppliesTo = append(d.AppliesTo, domain.IssueType(t))
	}
	if d.Options == nil {
		d.Options = []string{}
	}
	return d, nil
}

func (s *Store) ListFieldDefinitions(ctx context.Context, projectKey string) ([]domain.FieldDefinition, error) {
	rows, err := s.pool.Query(ctx, selectFieldDefinition+
		` WHERE p.key = $1 ORDER BY d.position, d.created_at`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FieldDefinition
	for rows.Next() {
		d, err := scanFieldDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) CreateFieldDefinition(ctx context.Context, in service.FieldDefinitionInput) (domain.FieldDefinition, error) {
	applies := make([]string, 0, len(in.AppliesTo))
	for _, t := range in.AppliesTo {
		applies = append(applies, string(t))
	}
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO field_definitions
		     (project_id, key, label, type, options, required, applies_to, help_text, position)
		 SELECT p.id, $2, $3, $4::field_type, $5, $6, $7::issue_type[], $8,
		        COALESCE((SELECT max(position) + 1 FROM field_definitions WHERE project_id = p.id), 0)
		   FROM projects p WHERE p.key = $1
		 RETURNING id`,
		in.ProjectKey, in.Key, in.Label, in.Type, in.Options, in.Required, applies, in.HelpText).
		Scan(&id); err != nil {
		return domain.FieldDefinition{}, err
	}
	return scanFieldDefinition(s.pool.QueryRow(ctx, selectFieldDefinition+` WHERE d.id = $1`, id))
}

// UpdateFieldDefinition edits the presentation of a field. The key and type are not
// editable: the key is what saved searches and API clients reference, and changing the
// type would leave every existing value in the wrong column with no way to convert it.
func (s *Store) UpdateFieldDefinition(ctx context.Context, id uuid.UUID, in service.UpdateFieldDefinitionInput) (domain.FieldDefinition, error) {
	var applies *[]string
	if in.AppliesTo != nil {
		converted := make([]string, 0, len(*in.AppliesTo))
		for _, t := range *in.AppliesTo {
			converted = append(converted, string(t))
		}
		applies = &converted
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE field_definitions SET
		   label      = COALESCE($2, label),
		   options    = COALESCE($3, options),
		   required   = COALESCE($4, required),
		   applies_to = COALESCE($5::issue_type[], applies_to),
		   help_text  = COALESCE($6, help_text),
		   position   = COALESCE($7, position)
		 WHERE id = $1`,
		id, in.Label, in.Options, in.Required, applies, in.HelpText, in.Position)
	if err != nil {
		return domain.FieldDefinition{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.FieldDefinition{}, pgx.ErrNoRows
	}
	return scanFieldDefinition(s.pool.QueryRow(ctx, selectFieldDefinition+` WHERE d.id = $1`, id))
}

func (s *Store) DeleteFieldDefinition(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM field_definitions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// IssueFieldValues reads one issue's custom fields, including the ones that are defined
// and unset — the form has to render every field that applies, not only the answered
// ones, or a required field nobody filled in becomes invisible.
func (s *Store) IssueFieldValues(ctx context.Context, issueID uuid.UUID) ([]domain.FieldValue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.key, d.label, d.type::text,
		       v.text_value, v.number_value, to_char(v.date_value, 'YYYY-MM-DD'),
		       v.user_value, v.bool_value, v.text_values,
		       u.display_name, u.email
		  FROM issues i
		  JOIN field_definitions d ON d.project_id = i.project_id
		  LEFT JOIN issue_field_values v ON v.definition_id = d.id AND v.issue_id = i.id
		  LEFT JOIN users u ON u.id = v.user_value
		 WHERE i.id = $1
		   AND (cardinality(d.applies_to) = 0 OR i.type = ANY(d.applies_to))
		 ORDER BY d.position, d.created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FieldValue
	for rows.Next() {
		var fv domain.FieldValue
		var text, dateStr, userName, userEmail *string
		var number *float64
		var userID *uuid.UUID
		var boolean *bool
		var texts []string
		if err := rows.Scan(&fv.Key, &fv.Label, &fv.Type, &text, &number, &dateStr,
			&userID, &boolean, &texts, &userName, &userEmail); err != nil {
			return nil, err
		}
		// Exactly one column carries the value, chosen by the definition's type. nil
		// stays nil: "not set" is a different answer from "" or 0 or false.
		switch fv.Type {
		case domain.FieldNumber:
			if number != nil {
				fv.Value = *number
			}
		case domain.FieldDate:
			if dateStr != nil {
				fv.Value = *dateStr
			}
		case domain.FieldCheckbox:
			if boolean != nil {
				fv.Value = *boolean
			}
		case domain.FieldMultiSelect:
			if texts != nil {
				fv.Value = texts
			}
		case domain.FieldUser:
			if userID != nil {
				fv.Value = userID.String()
				fv.User = &domain.User{
					ID: *userID, DisplayName: deref(userName), Email: deref(userEmail),
				}
			}
		default:
			if text != nil {
				fv.Value = *text
			}
		}
		out = append(out, fv)
	}
	return out, rows.Err()
}

// SetIssueFieldValues writes a batch of custom field values for one issue.
//
// Empty means "clear this field" rather than "skip it": the form always submits every
// field it rendered, so an omitted key is a field the caller did not have — and
// silently keeping a stale value in that case is how a field nobody can see any more
// keeps filtering issues.
func (s *Store) SetIssueFieldValues(ctx context.Context, issueID uuid.UUID, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Resolve keys to definitions in the issue's own project. Doing it here rather
	// than trusting the caller means a key from another project cannot be written.
	defs := map[string]struct {
		id  uuid.UUID
		typ string
	}{}
	rows, err := tx.Query(ctx,
		`SELECT d.id, d.key, d.type::text FROM field_definitions d
		   JOIN issues i ON i.project_id = d.project_id WHERE i.id = $1`, issueID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var key, typ string
		if err := rows.Scan(&id, &key, &typ); err != nil {
			rows.Close()
			return err
		}
		defs[key] = struct {
			id  uuid.UUID
			typ string
		}{id, typ}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for key, raw := range values {
		def, ok := defs[key]
		if !ok {
			return fmt.Errorf("unknown field %q for this project", key)
		}
		if isEmptyFieldValue(raw) {
			if _, err := tx.Exec(ctx,
				`DELETE FROM issue_field_values WHERE issue_id = $1 AND definition_id = $2`,
				issueID, def.id); err != nil {
				return err
			}
			continue
		}
		cols, err := fieldColumns(def.typ, raw)
		if err != nil {
			return fmt.Errorf("field %q: %w", key, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_field_values
			    (issue_id, definition_id, text_value, number_value, date_value,
			     user_value, bool_value, text_values, updated_at)
			VALUES ($1,$2,$3,$4,$5::date,$6::uuid,$7,$8, now())
			ON CONFLICT (issue_id, definition_id) DO UPDATE SET
			    text_value = EXCLUDED.text_value, number_value = EXCLUDED.number_value,
			    date_value = EXCLUDED.date_value, user_value = EXCLUDED.user_value,
			    bool_value = EXCLUDED.bool_value, text_values = EXCLUDED.text_values,
			    updated_at = now()`,
			issueID, def.id, cols.text, cols.number, cols.date, cols.user, cols.boolean, cols.texts,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type fieldCols struct {
	text    *string
	number  *float64
	date    *string
	user    *string
	boolean *bool
	texts   []string
}

// fieldColumns routes one value into the single column its type owns. Everything is
// bound explicitly as a typed pointer: leaving Postgres to infer a bare parameter is
// the 42P08 class of failure this codebase has already been bitten by three times.
func fieldColumns(fieldType string, raw any) (fieldCols, error) {
	var c fieldCols
	switch fieldType {
	case domain.FieldNumber:
		n, ok := toFloat(raw)
		if !ok {
			return c, fmt.Errorf("expected a number")
		}
		c.number = &n
	case domain.FieldCheckbox:
		b, ok := raw.(bool)
		if !ok {
			return c, fmt.Errorf("expected true or false")
		}
		c.boolean = &b
	case domain.FieldMultiSelect:
		items, ok := toStrings(raw)
		if !ok {
			return c, fmt.Errorf("expected a list of strings")
		}
		c.texts = items
	case domain.FieldDate:
		s, ok := raw.(string)
		if !ok {
			return c, fmt.Errorf("expected a YYYY-MM-DD date")
		}
		c.date = &s
	case domain.FieldUser:
		s, ok := raw.(string)
		if !ok {
			return c, fmt.Errorf("expected a user uuid")
		}
		if _, err := uuid.Parse(s); err != nil {
			return c, fmt.Errorf("expected a user uuid")
		}
		c.user = &s
	default:
		s, ok := raw.(string)
		if !ok {
			return c, fmt.Errorf("expected a string")
		}
		c.text = &s
	}
	return c, nil
}

func isEmptyFieldValue(raw any) bool {
	switch v := raw.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case []string:
		return len(v) == 0
	}
	return false
}

func toFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func toStrings(raw any) ([]string, bool) {
	switch v := raw.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// ResolveFieldFilter turns a `field:<key>:<value>` term into a SQL predicate against
// issue_field_values, plus the arguments it needs.
//
// Returns ok=false with a message when the key is unknown for the project, so the
// caller can answer 422 naming the field — the rule the enum terms already follow —
// rather than letting a failed cast surface as a 500.
func (s *Store) ResolveFieldFilter(ctx context.Context, projectKey, key, value string) (service.FieldPredicate, error) {
	var defID uuid.UUID
	var fieldType string
	err := s.pool.QueryRow(ctx,
		`SELECT d.id, d.type::text FROM field_definitions d JOIN projects p ON p.id = d.project_id
		  WHERE p.key = $1 AND d.key = $2`, projectKey, key).Scan(&defID, &fieldType)
	if err == pgx.ErrNoRows {
		return service.FieldPredicate{Problem: "no field named " + key + " in this project"}, nil
	}
	if err != nil {
		return service.FieldPredicate{}, err
	}

	p := service.FieldPredicate{DefinitionID: defID, Type: fieldType}
	switch fieldType {
	case domain.FieldNumber:
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%g", &n); err != nil {
			p.Problem = key + " is a number field — expected a number, got " + `"` + value + `"`
			return p, nil
		}
		p.Column, p.Arg = "number_value", n
	case domain.FieldCheckbox:
		switch strings.ToLower(value) {
		case "true", "yes", "1":
			p.Column, p.Arg = "bool_value", true
		case "false", "no", "0":
			p.Column, p.Arg = "bool_value", false
		default:
			p.Problem = key + " is a checkbox — expected true or false"
		}
	case domain.FieldDate:
		p.Column, p.Arg = "date_value", value
		p.Cast = "::date"
	case domain.FieldUser:
		id, err := uuid.Parse(value)
		if err != nil {
			p.Problem = key + " is a user field — expected a user uuid"
			return p, nil
		}
		p.Column, p.Arg = "user_value", id
	case domain.FieldMultiSelect:
		p.Column, p.Arg = "text_values", value
		p.ArrayContains = true
	default:
		p.Column, p.Arg = "text_value", value
	}
	return p, nil
}
