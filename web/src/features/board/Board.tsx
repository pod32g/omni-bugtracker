import { useEffect, useMemo, useState, type DragEvent } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Board as BoardConfig, type BoardColumn, type Issue, type IssueStatus } from "../../lib/api";
import { useProject } from "../../lib/project";
import { issueKeys } from "../../lib/queryKeys";
import { Avatar, LabelChip, PriorityText, SeverityBar, statusLabel } from "../../components/Badges";
import { IconGear, IconPlus } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);
const ALL_STATUSES: IssueStatus[] = [
  "open", "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened",
];
const BOARD_PAGE_SIZE = 200; // the API's per-request ceiling
// Upper bound on how much a board will pull. Past this the view is unusable anyway;
// the header says so explicitly rather than quietly rendering a partial board.
const BOARD_MAX_ISSUES = 2000;

export function Board() {
  const { projectKey } = useProject();
  const qc = useQueryClient();
  const [configuring, setConfiguring] = useState(false);

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const iterations = useQuery({
    queryKey: ["iterations", projectKey],
    queryFn: () => api.listIterations(projectKey),
    enabled: !!projectKey,
  });
  const activeIteration = iterations.data?.items.find((i) => i.state === "active");
  // Scoped to the current iteration by default when there is one: a board showing the
  // whole project (UMAOS is 472 issues) is a list with extra steps. `scope` is state
  // rather than derived, so the toggle survives the iterations query refetching.
  const [scope, setScope] = useState<"iteration" | "all">("iteration");
  const iterationScoped = scope === "iteration" && !!activeIteration;
  const boardFilter = iterationScoped ? `iteration:"${activeIteration.name}"` : "";
  const board = useQuery({
    queryKey: ["board", projectKey],
    queryFn: () => api.getBoard(projectKey),
    enabled: !!projectKey,
  });
  // A board must show the whole project: column contents and WIP warnings are
  // computed from what's loaded, so a partial page renders a quietly wrong board.
  // Pages until every issue is in hand (the API caps a single request at 200).
  const issues = useInfiniteQuery({
    queryKey: issueKeys.board(projectKey, boardFilter),
    queryFn: ({ pageParam }) => api.listIssues(projectKey, boardFilter, "rank", BOARD_PAGE_SIZE, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < Math.min(last.total, BOARD_MAX_ISSUES) ? loaded : undefined;
    },
    enabled: !!projectKey,
  });

  // Keep pulling pages until the project is fully loaded or the ceiling is hit.
  //
  // A failed page leaves `hasNextPage` true, so this must not re-fire on failure:
  // `isFetchingNextPage` flipping back to false is itself a dependency change, which
  // turns one broken page into an unbounded request loop against the same endpoint —
  // the failure mode the inbox poller documents at 30s intervals, here with no delay
  // at all. `isFetchNextPageError` latches until a manual retry succeeds.
  useEffect(() => {
    if (issues.hasNextPage && !issues.isFetchingNextPage && !issues.isFetchNextPageError) {
      issues.fetchNextPage();
    }
  }, [issues.hasNextPage, issues.isFetchingNextPage, issues.isFetchNextPageError, issues.fetchNextPage]);

  const transition = useMutation({
    mutationFn: ({ key, to }: { key: string; to: IssueStatus }) => api.transition(key, to),
    onSuccess: () => qc.invalidateQueries({ queryKey: issueKeys.all }),
  });
  // Ranking is a separate call from the transition: a card can move column, position,
  // or both, and a drop that only reorders must not pretend to be a status change.
  const rank = useMutation({
    mutationFn: ({ key, after, before }: { key: string; after: string; before: string }) =>
      api.rankIssue(key, after, before),
    onSuccess: () => qc.invalidateQueries({ queryKey: issueKeys.all }),
  });

  const items = issues.data?.pages.flatMap((p) => p.items) ?? [];
  const total = issues.data?.pages[0]?.total ?? 0;
  const truncated = items.length < total;
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

  /**
   * A drop carries both the column and the position within it. `atIndex` is where the
   * card landed among that column's cards as currently rendered; the two keys either
   * side of it are what the server ranks between.
   */
  const onDropAt = (col: BoardColumn, colIssues: Issue[]) => (atIndex: number) => (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const key = e.dataTransfer.getData("text/plain");
    const cur = items.find((i) => i.key === key);
    if (!key || !cur) return;

    if (!col.statuses.includes(cur.status)) transition.mutate({ key, to: col.statuses[0] });

    // Neighbours are computed with the dragged card removed, so dropping it one slot
    // down from where it already is does not rank it against itself.
    const without = colIssues.filter((i) => i.key !== key);
    const target = Math.max(0, Math.min(atIndex, without.length));
    rank.mutate({
      key,
      after: without[target - 1]?.key ?? "",
      before: without[target]?.key ?? "",
    });
  };

  return (
    <div>
      <div className="sticky top-0 z-10 flex items-end justify-between border-b border-hairline bg-paper/80 px-4 md:px-9 pb-5 pt-7 backdrop-blur">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Board</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {issues.isLoading || issues.isFetchingNextPage
              ? `Loading… ${items.length}/${total}`
              : `Project ${projectKey || "—"} · ${items.length} issues`}
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

      {activeIteration && (
        <div className="flex flex-wrap items-center gap-3 border-b border-hairline px-4 md:px-9 py-3">
          <span className="font-mono text-[11px] uppercase tracking-caps text-graphite-soft">Scope</span>
          {(
            [
              ["iteration", activeIteration.name],
              ["all", "Whole project"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              onClick={() => setScope(value)}
              className={`rounded-full border px-3 py-1 text-xs font-semibold transition ${
                scope === value
                  ? "border-blueprint bg-blueprint-soft text-blueprint"
                  : "border-hairline text-graphite hover:border-graphite hover:text-ink"
              }`}
            >
              {label}
            </button>
          ))}
          <Link
            to={`/issues?filter=${encodeURIComponent("is:open iteration:none")}`}
            className="ml-auto text-xs font-semibold text-blueprint transition hover:opacity-80"
          >
            Backlog →
          </Link>
        </div>
      )}

      {configuring && board.data && <BoardConfigPanel board={board.data} projectKey={projectKey} />}

      {issues.isError && <div className="px-4 md:px-9 py-6 text-sm text-critical">{(issues.error as Error).message}</div>}
      {board.isError && <div className="px-4 md:px-9 py-6 text-sm text-critical">{(board.error as Error).message}</div>}
      {transition.isError && (
        <div className="px-4 md:px-9 py-3 text-sm text-critical">{(transition.error as Error).message}</div>
      )}
      {/*
        Truncation has two causes and they need different words. Hitting the cap is
        expected and the advice is to filter. A failed page is a broken board: columns
        and WIP counts are derived from what loaded, so the numbers on screen are
        wrong rather than merely partial, and saying "capped" there would report a
        fault as a limit.
      */}
      {issues.isFetchNextPageError && (
        <div className="flex flex-wrap items-center gap-3 px-4 md:px-9 py-3 text-sm text-critical">
          <span>
            Couldn&rsquo;t load all issues — showing {items.length} of {total}. Column counts and WIP
            warnings are incomplete.
          </span>
          <button
            type="button"
            onClick={() => issues.fetchNextPage()}
            className="rounded border border-critical px-2 py-1 font-medium hover:bg-critical/10"
          >
            Retry
          </button>
        </div>
      )}
      {truncated && !issues.isFetchingNextPage && !issues.isFetchNextPageError && (
        <div className="px-4 md:px-9 py-3 text-sm text-graphite">
          Showing {items.length} of {total} issues — this board is capped at {BOARD_MAX_ISSUES}. Use the
          issue list with a filter to narrow it down.
        </div>
      )}

      <div className="flex flex-col gap-6 px-4 md:px-9 py-6">
        {lanes.map((lane) => (
          <div key={lane.label || "all"} className="flex flex-col gap-2">
            {lane.label && (
              <span className="font-mono text-[11px] font-semibold uppercase tracking-caps text-graphite">
                {lane.label} · {lane.issues.length}
              </span>
            )}
            <div className="flex snap-x snap-mandatory items-stretch gap-4 overflow-x-auto md:snap-none">
              {columns.map((col) => (
                <Column
                  key={col.id}
                  column={col}
                  issues={lane.issues.filter((i) => col.statuses.includes(i.status))}
                  totalInColumn={items.filter((i) => col.statuses.includes(i.status)).length}
                  compact={swimlane !== "none"}
                  onDropAt={onDropAt(col, lane.issues.filter((i) => col.statuses.includes(i.status)))}
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
  onDropAt,
}: {
  column: BoardColumn;
  issues: Issue[];
  totalInColumn: number;
  compact: boolean;
  onDropAt: (atIndex: number) => (e: DragEvent) => void;
}) {
  const overWip = column.wip_limit != null && totalInColumn > column.wip_limit;
  return (
    <div
      onDragOver={(e) => e.preventDefault()}
      // Dropping on the column background, not on a gap, means "the end".
      onDrop={onDropAt(issues.length)}
      // The row is items-stretch, so columns already match the tallest one; the
      // min-height only has to keep an all-empty board droppable. It used to reserve
      // a full viewport per column, which is why a board with three empty columns was
      // three screens of nothing.
      className={`flex w-[85vw] max-w-[300px] shrink-0 snap-start flex-col gap-3 rounded-lg border p-3 sm:w-[300px] ${
        overWip ? "border-critical/50 bg-critical-soft/30" : "border-hairline bg-panel"
      } ${compact ? "min-h-[120px]" : "min-h-[180px]"}`}
    >
      <div className="flex items-center justify-between px-1">
        <span className="text-sm font-semibold text-ink" title={column.statuses.map((s) => statusLabel[s]).join(", ")}>
          {column.name}
        </span>
        <span className={`font-mono text-xs ${overWip ? "font-bold text-critical" : "text-graphite-soft"}`}>
          {column.wip_limit != null ? `${totalInColumn}/${column.wip_limit}` : issues.length}
        </span>
      </div>
      <div
        className={`flex flex-1 flex-col ${
          compact ? "" : "max-h-[calc(100vh-270px)] overflow-y-auto"
        }`}
      >
        {issues.map((i, index) => (
          <div key={i.id}>
            <DropGap onDrop={onDropAt(index)} />
            <BoardCard issue={i} />
          </div>
        ))}
        <DropGap onDrop={onDropAt(issues.length)} last />
        {issues.length === 0 && (
          <div className="grid min-h-[60px] flex-1 place-items-center rounded-md border border-dashed border-hairline text-xs text-graphite-soft">
            Drop here
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * DropGap is the target between two cards. It is a thin strip that grows and highlights
 * while a card is over it — without a visible landing zone, precise reordering is
 * guesswork, and the gap has to be reachable without being so tall it pushes the column
 * around whenever nothing is being dragged.
 */
function DropGap({ onDrop, last = false }: { onDrop: (e: DragEvent) => void; last?: boolean }) {
  const [over, setOver] = useState(false);
  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        e.stopPropagation();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        setOver(false);
        onDrop(e);
      }}
      className={`rounded transition-all ${over ? "my-1 h-6 bg-blueprint/30" : last ? "h-2" : "h-1.5"}`}
    />
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
    <div className="flex flex-col gap-3 border-b border-hairline bg-panel/60 px-4 md:px-9 py-4">
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
      className="flex cursor-grab flex-col gap-2 rounded-md border border-hairline bg-paper p-2.5 transition hover:border-graphite active:cursor-grabbing"
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
          {issue.labels.slice(0, 2).map((l) => (
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
