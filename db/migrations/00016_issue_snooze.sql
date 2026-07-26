-- +goose Up
-- +goose StatementBegin

-- Snooze: an issue you cannot act on yet. Previously the only options were leave it
-- open and let it clutter every queue, close it and lose it, or archive it — which
-- hides it with nothing to bring it back.
--
-- Distinct from archived_at (indefinite, manual to reverse) and from status: a snoozed
-- issue is still open, it is simply not now.
ALTER TABLE issues ADD COLUMN snoozed_until TIMESTAMPTZ;
ALTER TABLE issues ADD COLUMN snooze_note   TEXT NOT NULL DEFAULT '';

-- The waking job scans only what is due, so a partial index keeps it O(due) rather
-- than O(issues).
CREATE INDEX idx_issues_snoozed ON issues (snoozed_until)
    WHERE snoozed_until IS NOT NULL AND deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_issues_snoozed;
ALTER TABLE issues DROP COLUMN IF EXISTS snooze_note;
ALTER TABLE issues DROP COLUMN IF EXISTS snoozed_until;
-- +goose StatementEnd
