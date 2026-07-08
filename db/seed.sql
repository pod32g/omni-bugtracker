-- Dev seed (not a migration). Load after `make migrate`:
--   psql "$DATABASE_URL" -f db/seed.sql
-- Creates a demo user + project so the SPA's default "BUG" project resolves.

INSERT INTO users (id, identity_sub, email, display_name, role)
VALUES ('00000000-0000-0000-0000-0000000000a1', 'demo|owner', 'owner@omni.dev', 'Demo Owner', 'owner')
ON CONFLICT (identity_sub) DO NOTHING;

INSERT INTO projects (key, name, description_md, default_assignee_id)
VALUES ('BUG', 'Bug Tracker', 'Default demo project.', '00000000-0000-0000-0000-0000000000a1')
ON CONFLICT (key) DO NOTHING;

INSERT INTO components (project_id, name, description_md)
SELECT id, 'Authentication', 'Auth & identity.' FROM projects WHERE key = 'BUG'
ON CONFLICT (project_id, name) DO NOTHING;

INSERT INTO labels (project_id, name, color)
SELECT id, 'regression', '#ef4444' FROM projects WHERE key = 'BUG'
ON CONFLICT DO NOTHING;
