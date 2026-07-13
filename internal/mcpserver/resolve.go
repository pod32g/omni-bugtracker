package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// userRef is the subset of a tracker user we need to resolve assignees by email.
type userRef struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

// resolveAssignee turns an optional (assignee_id, assignee_email) pair into a
// user-id string suitable for a request body's assignee_id field:
//
//   - neither given          → (nil, nil)  — caller omits assignee entirely
//   - id given               → validated uuid, passed through
//   - email given            → looked up case-insensitively via GET /users
//
// A non-matching or ambiguous email is a hard error listing candidates, so the
// AI gets an actionable message instead of a silently-wrong assignment.
func (s *Server) resolveAssignee(ctx context.Context, id, email string) (*string, error) {
	id = strings.TrimSpace(id)
	email = strings.TrimSpace(email)
	switch {
	case id != "":
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("assignee_id %q is not a valid uuid: %w", id, err)
		}
		return &id, nil
	case email != "":
		return s.userIDByEmail(ctx, email)
	default:
		return nil, nil
	}
}

func (s *Server) userIDByEmail(ctx context.Context, email string) (*string, error) {
	users, err := s.listUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve assignee_email: %w", err)
	}
	want := strings.ToLower(email)
	var matches []userRef
	for _, u := range users {
		if strings.ToLower(u.Email) == want {
			matches = append(matches, u)
		}
	}
	switch len(matches) {
	case 1:
		s := matches[0].ID.String()
		return &s, nil
	case 0:
		return nil, fmt.Errorf("no user with email %q (known emails: %s)", email, emailList(users))
	default:
		return nil, fmt.Errorf("email %q matches %d users; specify assignee_id instead", email, len(matches))
	}
}

func (s *Server) listUsers(ctx context.Context) ([]userRef, error) {
	raw, err := s.c.get(ctx, "/users", nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Items []userRef `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	return envelope.Items, nil
}

func emailList(users []userRef) string {
	if len(users) == 0 {
		return "none"
	}
	emails := make([]string, 0, len(users))
	for _, u := range users {
		emails = append(emails, u.Email)
	}
	return strings.Join(emails, ", ")
}
