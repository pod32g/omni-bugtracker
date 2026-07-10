import { useMemo, useState, type DragEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Board as BoardConfig, type BoardColumn, type Issue, type IssueStatus } from "../../lib/api";
import { useProject } from "../../lib/project";
import { Avatar, LabelChip, PriorityText, SeverityBar, statusLabel } from "../../components/Badges";
import { IconGear, IconPlus } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);
const ALL_STATUSES: IssueStatus[] = [
  "open", "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened",
];

export function Board() {
  const { projectKey } = useProject();
  const qc = useQueryClient();
  const [configuring, setConfiguring] = useState(false);

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const board = useQuery({
    queryKey: ["board", projectKey],
    queryFn: () => api.getBoard(projectKey),
    enabled: !!projectKey,
  });
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
  const columns = board.data?.columns ?? [];
  const swimlane = board.data?.swimlane ?? "none";

  // Lanes: one pseudo-lane when swimlanes are off, otherwise group by assignee/priority.
  const lanes = useMemo(() => {
    if (swimlane === "none") return [{ label: "", issues: items }];
    const groups = new Map<string, Issue[]>();
    for (const i of items) {
      const label = swimlane === "assignee" ? (i.assignee?.display_name ?? i.assignee?.email ?? "Unassigned") : i.priority.toUpperCase();
      groups.set(label, [...(groups.get(label) ?? []), i]);
    }
    return [...groups.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([label, laneIssues]) => ({ label, issues: laneIssues }));
  }, [items, swimlane]);

  const onDrop = (col: BoardColumn) => (e: DragEvent) => {
    e.preventDefault();
    const key = e.dataTransfer.getData("text/plain");
    const cur = items.find((i) => i.key === key);
    if (key && cur && !col.statuses.includes(cur.status)) transition.mutate({ key, to: col.statuses[0] });
  };

  return (
    <div>
      <div className="sticky top-0 z-10 flex items-end justify-between border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Board</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {issues.isLoading ? "Loading…" : `Project ${projectKey || "—"} · ${items.length} issues`}
            {swimlane !== "none" && ` · lanes by ${swimlane}`}
          </p>
        </div>
        <div className="flex items-center gap-3">
          {transition.isPending && <span className="font-mono text-xs text-graphite-soft">Moving…</span>}
          {canManage && board.data && (
            <button
              onClick={() => setConfiguring((c) => !c)}
              className={`flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition ${
                configuring ? "border-blueprint bg-blueprint-soft text-blueprint" : "border-hairline text-graphite hover:border-graphite hover:text-ink"
              }`}
            >
              <IconGear size={15} />
              Configure
            </button>
          )}
        </div>
      </div>

      {configuring && board.data && <BoardConfigPanel board={board.data} projectKey={projectKey} />}

      {issues.isError && <div className="px-9 py-6 text-sm text-critical">{(issues.error as Error).message}</div>}
      {board.isError && <div className="px-9 py-6 text-sm text-critical">{(board.error as Error).message}</div>}

      <div className="flex flex-col gap-6 px-9 py-6">
        {lanes.map((lane) => (
          <div key={lane.label || "all"} className="flex flex-col gap-2">
            {lane.label && (
              <span className="font-mono text-[11px] font-semibold uppercase tracking-caps text-graphite">
                {lane.label} · {lane.issues.length}
              </span>
            )}
            <div className="flex items-stretch gap-4 overflow-x-auto">
              {columns.map((col) => (
                <Column
                  key={col.id}
                  column={col}
                  issues={lane.issues.filter((i) => col.statuses.includes(i.status))}
                  totalInColumn={items.filter((i) => col.statuses.includes(i.status)).length}
                  compact={swimlane !== "none"}
                  onDrop={onDrop(col)}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Column({
  column,
  issues,
  totalInColumn,
  compact,
  onDrop,
}: {
  column: BoardColumn;
  issues: Issue[];
  totalInColumn: number;
  compact: boolean;
  onDrop: (e: DragEvent) => void;
}) {
  const overWip = column.wip_limit != null && totalInColumn > column.wip_limit;
  return (
    <div
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDrop}
      className={`flex w-[300px] shrink-0 flex-col gap-3 rounded-lg border p-3 ${
        overWip ? "border-critical/50 bg-critical-soft/30" : "border-hairline bg-panel"
      } ${compact ? "min-h-[120px]" : "min-h-[calc(100vh-200px)]"}`}
    >
      <div className="flex items-center justify-between px-1">
        <span className="text-sm font-semibold text-ink" title={column.statuses.map((s) => statusLabel[s]).join(", ")}>
          {column.name}
        </span>
        <span className={`font-mono text-xs ${overWip ? "font-bold text-critical" : "text-graphite-soft"}`}>
          {column.wip_limit != null ? `${totalInColumn}/${column.wip_limit}` : issues.length}
        </span>
      </div>
      <div className="flex flex-1 flex-col gap-2">
        {issues.map((i) => (
          <BoardCard key={i.id} issue={i} />
        ))}
        {issues.length === 0 && (
          <div className="grid min-h-[60px] flex-1 place-items-center rounded-md border border-dashed border-hairline text-xs text-graphite-soft">
            Drop here
          </div>
        )}
      </div>
    </div>
  );
}

// BoardConfigPanel edits swimlane + columns (rename, statuses, WIP, order, add/delete).
function BoardConfigPanel({ board, projectKey }: { board: BoardConfig; projectKey: string }) {
  const qc = useQueryClient();
  const invalidate = () => qc.invalidateQueries({ queryKey: ["board", projectKey] });
  const [newName, setNewName] = useState("");

  const setSwimlane = useMutation({
    mutationFn: (swimlane: BoardConfig["swimlane"]) => api.updateBoard(board.id, { swimlane }),
    onSuccess: invalidate,
  });
  const patchCol = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Parameters<typeof api.updateBoardColumn>[1] }) =>
      api.updateBoardColumn(id, patch),
    onSuccess: invalidate,
  });
  const addCol = useMutation({
    mutationFn: () => api.createBoardColumn(board.id, { name: newName.trim(), statuses: ["open"] }),
    onSuccess: () => {
      setNewName("");
      invalidate();
    },
  });
  const delCol = useMutation({ mutationFn: (id: string) => api.deleteBoardColumn(id), onSuccess: invalidate });

  const err = setSwimlane.error ?? patchCol.error ?? addCol.error ?? delCol.error;

  return (
    <div className="flex flex-col gap-3 border-b border-hairline bg-panel/60 px-9 py-4">
      <div className="flex items-center gap-3">
        <span className="font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft">Swimlanes</span>
        <select
          value={board.swimlane}
          onChange={(e) => setSwimlane.mutate(e.target.value as BoardConfig["swimlane"])}
          className="rounded-md border border-hairline bg-paper px-2 py-1.5 text-sm text-ink outline-none focus:border-blueprint"
        >
          <option value="none">none</option>
          <option value="assignee">by assignee</option>
          <option value="priority">by priority</option>
        </select>
      </div>

      <div className="flex flex-col gap-2">
        {board.columns.map((col, idx) => (
          <ColumnConfigRow
            key={col.id}
            column={col}
            isFirst={idx === 0}
            isLast={idx === board.columns.length - 1}
            onPatch={(patch) => patchCol.mutate({ id: col.id, patch })}
            onDelete={() => {
              if (window.confirm(`Delete column “${col.name}”? Issues keep their status.`)) delCol.mutate(col.id);
            }}
          />
        ))}
        <div className="flex items-center gap-2">
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && newName.trim()) addCol.mutate();
            }}
            placeholder="New column name…"
            className="w-48 rounded-md border border-hairline bg-paper px-2.5 py-1.5 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
          />
          <button
            disabled={!newName.trim() || addCol.isPending}
            onClick={() => addCol.mutate()}
            className="flex h-8 items-center gap-1 rounded-md bg-blueprint px-3 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            <IconPlus size={13} />
            Add column
          </button>
        </div>
      </div>
      {err && <p className="text-sm text-critical">{(err as Error).message}</p>}
    </div>
  );
}

function ColumnConfigRow({
  column,
  isFirst,
  isLast,
  onPatch,
  onDelete,
}: {
  column: BoardColumn;
  isFirst: boolean;
  isLast: boolean;
  onPatch: (patch: { name?: string; statuses?: IssueStatus[]; wip_limit?: number; position?: number }) => void;
  onDelete: () => void;
}) {
  const [name, setName] = useState(column.name);
  const toggleStatus = (s: IssueStatus) => {
    const next = column.statuses.includes(s)
      ? column.statuses.filter((v) => v !== s)
      : [...column.statuses, s];
    if (next.length > 0) onPatch({ statuses: next });
  };

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-hairline bg-paper px-3 py-2">
      <input
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={() => {
          const n = name.trim();
          if (n && n !== column.name) onPatch({ name: n });
          else setName(column.name);
        }}
        className="w-32 rounded-md border border-transparent bg-transparent px-1.5 py-1 text-sm font-semibold text-ink outline-none transition hover:border-hairline focus:border-blueprint"
      />
      <div className="flex flex-wrap gap-1">
        {ALL_STATUSES.map((s) => {
          const active = column.statuses.includes(s);
          return (
            <button
              key={s}
              onClick={() => toggleStatus(s)}
              className={`rounded-full border px-2 py-0.5 font-mono text-[10px] transition ${
                active
                  ? "border-blueprint bg-blueprint-soft text-blueprint"
                  : "border-hairline text-graphite-soft hover:border-graphite"
              }`}
            >
              {s}
            </button>
          );
        })}
      </div>
      <span className="grow" />
      <label className="flex items-center gap-1 font-mono text-xs text-graphite-soft">
        wip
        <input
          type="number"
          min={0}
          value={column.wip_limit ?? ""}
          placeholder="∞"
          onChange={(e) => onPatch({ wip_limit: e.target.value === "" ? -1 : Number(e.target.value) })}
          className="w-14 rounded-md border border-hairline bg-paper px-1.5 py-1 text-sm text-ink outline-none focus:border-blueprint"
        />
      </label>
      <button
        disabled={isFirst}
        onClick={() => onPatch({ position: column.position - 1 })}
        aria-label="Move column left"
        className="rounded-md border border-hairline px-2 py-1 text-xs text-graphite transition hover:border-graphite disabled:opacity-30"
      >
        ←
      </button>
      <button
        disabled={isLast}
        onClick={() => onPatch({ position: column.position + 1 })}
        aria-label="Move column right"
        className="rounded-md border border-hairline px-2 py-1 text-xs text-graphite transition hover:border-graphite disabled:opacity-30"
      >
        →
      </button>
      <button
        onClick={onDelete}
        className="rounded-md border border-hairline px-2 py-1 text-xs text-critical transition hover:border-critical"
      >
        ✕
      </button>
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
      {issue.open_blockers > 0 && (
        <span className="self-start rounded-full bg-critical-soft px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-caps text-critical">
          blocked · {issue.open_blockers}
        </span>
      )}
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
