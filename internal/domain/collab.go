package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID        uuid.UUID  `json:"id"`
	IssueID   uuid.UUID  `json:"issue_id"`
	Author    *User      `json:"author,omitempty"`
	BodyMD    string     `json:"body_md"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type Activity struct {
	ID         uuid.UUID       `json:"id"`
	IssueID    *uuid.UUID      `json:"issue_id,omitempty"`
	Actor      *User           `json:"actor,omitempty"`
	Verb       string          `json:"verb"`
	EntityType string          `json:"entity_type"`
	Changes    json.RawMessage `json:"changes,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
}
