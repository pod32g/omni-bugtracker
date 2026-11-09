import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type IssueStatus, type Priority } from "../lib/api";
import { useProject } from "../lib/project";
import { issueKeys } from "../lib/queryKeys";

// The bulk endpoint refuses more than 100 ids per request. A selection can be larger
// (a board's Done column, or a list paged past two "Load more"s), so it goes in
// batches of this size, one after another, and the results are added up.
const BULK_BATCH = 100;

type BulkBody = Parameters<typeof api.bulkUpdateIssues>[0];
type BulkResult = Awaited<ReturnType<typeof api.bulkUpdateIssues>>;

async function bulkUpdateInBatches(body: BulkBody): Promise<BulkResult> {
  const total: BulkResult = { updated: 0, skipped: 0, failed: [] };
  for (let at = 0; at < body.ids.length; at += BULK_BATCH) {
    let res: BulkResult;
    try {
      res = await api.bulkUpdateIssues({ ...body, ids: body.ids.slice(at, at + BULK_BATCH) });
    } catch (err) {
      // Earlier batches are already applied and there is no undo, so the error has to
      // say how far it got — "failed" alone would read as "nothing changed".
      if (at === 0) throw err;
      throw new Error(`Stopped after ${at} of ${body.ids.length}: ${(err as Error).message}`);
    }
    total.updated += res.updated;
    total.skipped += res.skipped;
    total.failed.push(...res.failed);
  }
  return total;
}

/**
 * Floating action bar shown while issues are selected, in the list and on the board.
 * Every action calls the bulk endpoint, reports failures, refreshes every issue query,
 * and calls onDone so the caller can clear its selection.
 */
export function BulkBar({ ids, projectKey, onDone }: { ids: string[]; projectKey: string; onDone: () => void }) {
  const qc = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const { projects } = useProject();
  // Only offer labels the project already has. Free text here would let a typo create
  // a new label across fifty issues at once, and there is no undo for that either.
  const labels = useQuery({
    queryKey: ["labels", projectKey],
    queryFn: () => api.listLabels(projectKey),
    enabled: !!projectKey,
  });

  const refresh = () => {
    qc.invalidateQueries({ queryKey: issueKeys.all });
    qc.invalidateQueries({ queryKey: ["dashboard"] });
  };

  const run = useMutation({
    mutationFn: bulkUpdateInBatches,
    // A run that fails part-way has still changed the batches before it.
    onError: refresh,
    onSuccess: (res) => {
      refresh();
      if (res.failed.length > 0)
        window.alert(
          `${res.updated} updated, ${res.skipped} unchanged, ${res.failed.length} failed:\n` +
            res.failed.map((f) => `${f.key}: ${f.error}`).join("\n"),
        );
      onDone();
    },
  });

  const selectClass =
    "h-8 rounded-md border border-hairline bg-paper px-2 text-sm text-ink outline-none focus:border-blueprint";

  return (
    <div className="fixed bottom-6 left-1/2 z-40 flex -translate-x-1/2 items-center gap-3 rounded-lg border border-hairline bg-paper px-4 py-3 shadow-xl shadow-ink/15">
      <span className="font-mono text-xs font-semibold text-blueprint">{ids.length} selected</span>
      <span className="h-5 w-px bg-hairline" />

      <select
        defaultValue=""
        aria-label="Bulk status"
        className={selectClass}
        onChange={(e) => e.target.value && run.mutate({ ids, status: e.target.value as IssueStatus })}
      >
        <option value="">Status…</option>
        {(["open", "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened"] as IssueStatus[]).map(
          (s) => (
            <option key={s} value={s}>
              {s.replace(/_/g, " ")}
            </option>
          ),
        )}
      </select>

      <select
        defaultValue=""
        aria-label="Bulk assignee"
        className={selectClass}
        onChange={(e) => e.target.value && run.mutate({ ids, patch: { assignee_id: e.target.value } })}
      >
        <option value="">Assign…</option>
        {users.data?.items.map((u) => (
          <option key={u.id} value={u.id}>
            {u.display_name || u.email}
          </option>
        ))}
      </select>

      <select
        defaultValue=""
        aria-label="Bulk priority"
        className={selectClass}
        onChange={(e) => e.target.value && run.mutate({ ids, patch: { priority: e.target.value as Priority } })}
      >
        <option value="">Priority…</option>
        {["p0", "p1", "p2", "p3"].map((p) => (
          <option key={p} value={p}>
            {p.toUpperCase()}
          </option>
        ))}
      </select>

      <select
        defaultValue=""
        aria-label="Bulk move"
        className={selectClass}
        onChange={(e) => {
          const key = e.target.value;
          if (!key) return;
          if (window.confirm(`Move ${ids.length} issue(s) to ${key}? They get new keys and lose milestone/release/components.`))
            run.mutate({ ids, target_project_key: key });
          else e.target.value = "";
        }}
      >
        <option value="">Move to…</option>
        {projects
          .filter((p) => p.key !== projectKey)
          .map((p) => (
            <option key={p.key} value={p.key}>
              {p.key}
            </option>
          ))}
      </select>

      {/* labels_add / labels_remove, never `labels`. `labels` replaces the whole set,
          so "add one label to these fifty" would strip every other label from all
          fifty — and unlike the single-issue form, the user cannot see what they are
          about to lose. */}
      {!!projectKey && (labels.data?.items.length ?? 0) > 0 && (
        <>
          <select
            defaultValue=""
            aria-label="Add label to selected issues"
            className={selectClass}
            onChange={(e) => {
              if (e.target.value) run.mutate({ ids, patch: { labels_add: [e.target.value] } });
              e.target.value = "";
            }}
          >
            <option value="">+ Label…</option>
            {labels.data?.items.map((l) => (
              <option key={l.id} value={l.name}>
                {l.name}
              </option>
            ))}
          </select>

          <select
            defaultValue=""
            aria-label="Remove label from selected issues"
            className={selectClass}
            onChange={(e) => {
              if (e.target.value) run.mutate({ ids, patch: { labels_remove: [e.target.value] } });
              e.target.value = "";
            }}
          >
            <option value="">− Label…</option>
            {labels.data?.items.map((l) => (
              <option key={l.id} value={l.name}>
                {l.name}
              </option>
            ))}
          </select>
        </>
      )}

      <button
        onClick={() => {
          if (window.confirm(`Archive ${ids.length} issue(s)? They drop out of default lists but stay recoverable.`))
            run.mutate({ ids, archived: true });
        }}
        className="h-8 shrink-0 rounded-md border border-hairline px-3 text-sm text-graphite transition hover:border-graphite hover:text-ink"
      >
        Archive
      </button>

      {run.isPending && <span className="font-mono text-xs text-graphite-soft">Applying…</span>}
      {run.isError && (
        <span className="max-w-[240px] truncate text-xs text-critical" title={(run.error as Error).message}>
          {(run.error as Error).message}
        </span>
      )}
      <button onClick={onDone} className="text-sm text-graphite transition hover:text-ink">
        Clear
      </button>
    </div>
  );
}
