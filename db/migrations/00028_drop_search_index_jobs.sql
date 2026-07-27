-- +goose Up
-- +goose StatementBegin

-- Omni-Search never existed. Search has always been native Postgres FTS over the
-- generated `fts` tsvector columns, but the projection worker from the original
-- scaffolding kept enqueuing `search_index` jobs at every issue write, each one
-- retrying against a host that does not resolve. On the live instance that had
-- accumulated 1794 retryable jobs with the circuit breaker permanently open — not
-- user-visible, but the queue grew without bound and a real failure would have been
-- buried under thousands of identical ones.
--
-- The worker and the adapter are gone as of this migration's commit. These rows would
-- otherwise sit in river_job forever: River cannot run a job whose kind is no longer
-- registered, so nothing would ever clear them.
--
-- Deliberately unconditional rather than restricted to 'retryable': a completed
-- search_index row is also a record of work that never actually happened.
DELETE FROM river_job WHERE kind = 'search_index';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Irreversible by design. These jobs projected into a service that does not exist;
-- recreating them would only recreate the failure.
SELECT 1;
-- +goose StatementEnd
