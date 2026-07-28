import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type SavedSearch } from "../../../lib/api";
import { useProject } from "../../../lib/project";
import { Card, ErrorLine, dangerButtonClass, inputClass, quietButtonClass } from "../ui";

/**
 * ViewsSection curates saved filters. Both kinds were creatable from the issue list
 * and manageable nowhere: a personal view could not be renamed at all (saving under a
 * new name just made a second one), and shared views could only be reviewed by walking
 * into each project's settings, which is why nobody reviewed them.
 */
export function ViewsSection() {
  return (
    <>
      <PersonalViews />
      <SharedViews />
    </>
  );
}

function PersonalViews() {
  const qc = useQueryClient();
  const { projects } = useProject();
  const views = useQuery({ queryKey: ["saved-searches"], queryFn: () => api.listSavedSearches() });
  const [editingId, setEditingId] = useState<string | null>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["saved-searches"] });
    qc.invalidateQueries({ queryKey: ["all-views"] });
  };
  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: { name?: string; query?: string } }) =>
      api.updateSavedSearch(id, patch),
    onSuccess: () => {
      setEditingId(null);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteSavedSearch(id), onSuccess: invalidate });
  const share = useMutation({
    mutationFn: ({ id, projectKey }: { id: string; projectKey: string }) => api.shareSavedSearch(id, projectKey),
    onSuccess: invalidate,
  });

  const items = views.data?.items ?? [];

  return (
    <Card
      title="My saved views"
      description={
        <>
          Named filters, private to you. Share one into a project to make it the team's — the view keeps its author
          and creation date, and stops being yours to edit alone. Build new ones from the{" "}
          <Link to="/issues" className="text-blueprint hover:underline">
            issue list
          </Link>
          .
        </>
      }
    >
      <ErrorLine error={del.error} />
      <ErrorLine error={share.error} />
      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {views.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {views.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No saved views yet.</div>
        )}
        {items.map((v) => (
          <div key={v.id} className="flex flex-col">
            {/* Wraps rather than shrinks — four controls against a min-w-0 name
                column left nothing but an initial on a phone. */}
            <div className="flex flex-wrap items-center gap-3 p-3.5">
              <div className="min-w-[10rem] grow basis-0">
                <div className="truncate text-sm font-medium text-ink">{v.name}</div>
                <code className="block truncate font-mono text-xs text-graphite-soft">{v.query}</code>
              </div>
              <Link
                to={`/issues?filter=${encodeURIComponent(v.query)}`}
                className="shrink-0 text-sm text-blueprint hover:underline"
              >
                Open
              </Link>
              <button onClick={() => setEditingId(editingId === v.id ? null : v.id)} className={quietButtonClass}>
                {editingId === v.id ? "Close" : "Edit"}
              </button>
              <ShareControl
                projects={projects.map((p) => p.key)}
                pending={share.isPending}
                onShare={(projectKey) => share.mutate({ id: v.id, projectKey })}
              />
              <button
                disabled={del.isPending}
                onClick={() => {
                  if (window.confirm(`Delete the saved view “${v.name}”?`)) del.mutate(v.id);
                }}
                className={dangerButtonClass}
              >
                Delete
              </button>
            </div>
            {editingId === v.id && (
              <ViewEditor
                view={v}
                pending={update.isPending}
                error={update.error as Error | null}
                onSave={(patch) => update.mutate({ id: v.id, patch })}
                onCancel={() => setEditingId(null)}
              />
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}

function SharedViews() {
  const qc = useQueryClient();
  const views = useQuery({ queryKey: ["all-views"], queryFn: () => api.listAllViews() });
  const [editingId, setEditingId] = useState<string | null>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["all-views"] });
    qc.invalidateQueries({ queryKey: ["project-views"] });
  };
  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: { name?: string; query?: string } }) =>
      api.updateProjectView(id, patch),
    onSuccess: () => {
      setEditingId(null);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteProjectView(id), onSuccess: invalidate });

  const items = views.data?.items ?? [];

  return (
    <Card
      title="Shared views"
      description="Every project's shared views, in one list. A shared view is a claim about how the team works — a stale one quietly sends people to the wrong queue, so editing and deleting take project:manage."
    >
      <ErrorLine error={views.error} />
      <ErrorLine error={del.error} />
      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {views.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {views.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No shared views yet.</div>
        )}
        {items.map((v) => (
          <div key={v.id} className="flex flex-col">
            <div className="flex flex-wrap items-center gap-3 p-3.5">
              <span className="shrink-0 rounded-sm bg-panel px-1.5 py-0.5 font-mono text-[10px] text-graphite">
                {v.project_key || "—"}
              </span>
              <div className="min-w-[10rem] grow basis-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-ink">{v.name}</span>
                  {v.is_default && (
                    <span className="shrink-0 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                      default
                    </span>
                  )}
                </div>
                <code className="block truncate font-mono text-xs text-graphite-soft">{v.query}</code>
              </div>
              {v.author && (
                <span className="hidden shrink-0 text-xs text-graphite-soft sm:block">
                  {v.author.display_name || v.author.email}
                </span>
              )}
              <button onClick={() => setEditingId(editingId === v.id ? null : v.id)} className={quietButtonClass}>
                {editingId === v.id ? "Close" : "Edit"}
              </button>
              <button
                disabled={del.isPending}
                onClick={() => {
                  if (window.confirm(`Delete the shared view “${v.name}”? Everyone in ${v.project_key} loses it.`))
                    del.mutate(v.id);
                }}
                className={dangerButtonClass}
              >
                Delete
              </button>
            </div>
            {editingId === v.id && (
              <ViewEditor
                view={v}
                pending={update.isPending}
                error={update.error as Error | null}
                onSave={(patch) => update.mutate({ id: v.id, patch })}
                onCancel={() => setEditingId(null)}
              />
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}

function ViewEditor({
  view,
  pending,
  error,
  onSave,
  onCancel,
}: {
  view: SavedSearch;
  pending: boolean;
  error: Error | null;
  onSave: (patch: { name: string; query: string }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(view.name);
  const [query, setQuery] = useState(view.query);

  return (
    <div className="flex flex-col gap-2 border-t border-hairline bg-panel/50 p-3.5">
      <div className="flex flex-wrap items-end gap-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Name"
          className={`${inputClass} w-48`}
        />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="is:open assignee:@me"
          className={`${inputClass} grow font-mono`}
        />
        <button
          disabled={!name.trim() || !query.trim() || pending}
          onClick={() => onSave({ name: name.trim(), query: query.trim() })}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {pending ? "Saving…" : "Save view"}
        </button>
        <button onClick={onCancel} className="h-[38px] px-3 text-sm text-graphite transition hover:text-ink">
          Cancel
        </button>
      </div>
      {/* The server parses the filter before storing it, so a typo is refused here
          rather than discovered by whoever opens the view next. */}
      <ErrorLine error={error} />
    </div>
  );
}

function ShareControl({
  projects,
  pending,
  onShare,
}: {
  projects: string[];
  pending: boolean;
  onShare: (projectKey: string) => void;
}) {
  const [open, setOpen] = useState(false);
  if (!projects.length) return null;
  if (!open) {
    return (
      <button onClick={() => setOpen(true)} className={quietButtonClass}>
        Share
      </button>
    );
  }
  return (
    <select
      autoFocus
      defaultValue=""
      disabled={pending}
      aria-label="Share into project"
      onChange={(e) => {
        if (e.target.value) onShare(e.target.value);
        setOpen(false);
      }}
      onBlur={() => setOpen(false)}
      className="h-[34px] shrink-0 rounded-md border border-hairline bg-paper px-2 text-sm text-ink outline-none focus:border-blueprint"
    >
      <option value="">Share into…</option>
      {projects.map((k) => (
        <option key={k} value={k}>
          {k}
        </option>
      ))}
    </select>
  );
}
