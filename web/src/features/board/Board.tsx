import { useMemo, type DragEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Issue, type IssueStatus } from "../../lib/api";
import { useProject } from "../../lib/project";
import { Avatar, LabelChip, PriorityText, SeverityBar, statusLabel, statusTone } from "../../components/Badges";

// Board columns in workflow order. Cards drag between columns to transition status.
const COLUMNS: IssueStatus[] = ["open", "in_progress", "blocked", "ready_for_review", "resolved", "closed"];

export function Board() {
  const { projectKey } = useProject();
  const qc = useQueryClient();

  const issues = useQuery({
    queryKey: ["issues", projectKey, "", "board"],
    queryFn: () => api.listIssues(projectKey, "", ""),
    enabled: !!projectKey,
  });

  const transition = useMutation({
    mutationFn: ({ key, to }: { key: string; to: IssueStatus }) => api.transition(key, to),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  const items = issues.data?.items ?? [];
  const byStatus = useMemo(() => {
    const m: Record<string, Issue[]> = Object.fromEntries(COLUMNS.map((c) => [c, [] as Issue[]]));
    for (const i of items) (m[i.status] ??= []).push(i);
    return m;
  }, [items]);

  const onDrop = (to: IssueStatus) => (e: DragEvent) => {
    e.preventDefault();
    const key = e.dataTransfer.getData("text/plain");
    const cur = items.find((i) => i.key === key);
    if (key && cur && cur.status !== to) transition.mutate({ key, to });
  };

  return (
    <div>
      <div className="sticky top-0 z-10 flex items-end justify-between border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Board</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {issues.isLoading ? "Loading…" : `Project ${projectKey || "—"} · ${items.length} issues`}
          </p>
        </div>
        {transition.isPending && <span className="font-mono text-xs text-graphite-soft">Moving…</span>}
      </div>

      {issues.isError && <div className="px-9 py-6 text-sm text-critical">{(issues.error as Error).message}</div>}
      <div className="flex min-h-[calc(100vh-92px)] items-stretch gap-4 overflow-x-auto px-9 py-6">
        {COLUMNS.map((col) => (
          <Column key={col} status={col} issues={byStatus[col] ?? []} onDrop={onDrop(col)} />
        ))}
      </div>
    </div>
  );
}

function Column({
  status,
  issues,
  onDrop,
}: {
  status: IssueStatus;
  issues: Issue[];
  onDrop: (e: DragEvent) => void;
}) {
  const t = statusTone[status];
  return (
    <div
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDrop}
      className="flex w-[300px] shrink-0 flex-col gap-3 rounded-lg border border-hairline bg-panel p-3"
    >
      <div className="flex items-center justify-between px-1">
        <span className="flex items-center gap-2">
          <span className={`h-1.5 w-1.5 rounded-full ${t.dot}`} />
          <span className="text-sm font-semibold text-ink">{statusLabel[status]}</span>
        </span>
        <span className="font-mono text-xs text-graphite-soft">{issues.length}</span>
      </div>
      <div className="flex flex-1 flex-col gap-2">
        {issues.map((i) => (
          <BoardCard key={i.id} issue={i} />
        ))}
        {issues.length === 0 && (
          <div className="grid flex-1 place-items-center rounded-md border border-dashed border-hairline text-xs text-graphite-soft">
            Drop here
          </div>
        )}
      </div>
    </div>
  );
}

function BoardCard({ issue }: { issue: Issue }) {
  return (
    <Link
      to={`/issues/${issue.key}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("text/plain", issue.key);
        e.dataTransfer.effectAllowed = "move";
      }}
      className="flex cursor-grab flex-col gap-2.5 rounded-md border border-hairline bg-paper p-3 transition hover:border-graphite active:cursor-grabbing"
    >
      <div className="flex items-start gap-2">
        <SeverityBar severity={issue.severity} />
        <span className="grow text-sm font-medium leading-tight text-ink">{issue.title}</span>
      </div>
      {issue.labels && issue.labels.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {issue.labels.slice(0, 3).map((l) => (
            <LabelChip key={l} name={l} />
          ))}
        </div>
      )}
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-2">
          <span className="font-mono text-xs font-medium text-blueprint">{issue.key}</span>
          <PriorityText priority={issue.priority} />
        </span>
        <Avatar user={issue.assignee} size={22} />
      </div>
    </Link>
  );
}
