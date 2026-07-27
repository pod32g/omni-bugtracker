import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  api,
  type EffortRollup,
  type Issue,
  type IssueStatus,
  type Priority,
  type SavedSearch,
} from "../../lib/api";
import { useProject } from "../../lib/project";
import { useShortcut } from "../../lib/shortcuts";
import { timeAgo } from "../../lib/activity";
import { sameUnit } from "../../lib/duration";
import {
  Avatar,
  ChecklistChip,
  DueChip,
  EstimateChip,
  LabelChip,
  PriorityText,
  SeverityBar,
  SeverityMark,
  SLAPill,
  StatusPill,
} from "../../components/Badges";
import { IconArrowDown, IconLabelLines, IconPlus, IconSearch } from "../../components/icons";
import { NewIssueForm } from "./NewIssueForm";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);
const PAGE_SIZE = 50;

const QUICK_FILTERS = [
  { label: "Open", filter: "is:open" },
  { label: "Assigned to me", filter: "assignee:@me" },
  { label: "Critical", filter: "severity:critical" },
  { label: "All", filter: "" },
  { label: "Archived", filter: "is:archived" },
  { label: "Snoozed", filter: "is:snoozed" },
  { label: "Overdue", filter: "is:open due:overdue" },
  { label: "Over budget", filter: "is:open over-budget:true" },
  { label: "This iteration", filter: "is:open iteration:current" },
];

const SORTS = [
  { value: "", label: "Newest" },
  { value: "created_at", label: "Oldest" },
  { value: "-updated_at", label: "Recently updated" },
  { value: "priority", label: "Priority" },
  { value: "severity", label: "Severity" },
  { value: "due", label: "Due date" },
];


// Compact relative time for the dense list ("2h", "5h", "1d", "just now").
const shortAgo = (iso: string) => timeAgo(iso).replace(" ago", "");

export function IssueList() {
  const { projects, projectKey } = useProject();
  const [searchParams, setSearchParams] = useSearchParams();
  // Applied at most once per project: after that the filter is the user's, and
  // re-applying a default over what somebody typed would be maddening.
  const appliedDefaultFor = useRef<string | null>(null);
  // Initial filter can be deep-linked from the dashboard gadgets (e.g. ?filter=assignee:@me).
  const [filter, setFilter] = useState(() => searchParams.get("filter") ?? "is:open");
  const [sort, setSort] = useState("");
  // ?new=1 opens the composer, so the "c" shortcut can reach it from any page and
  // "file an issue here" is a link somebody can send.
  const [showNewIssue, setShowNewIssue] = useState(() => searchParams.get("new") === "1");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // -1 = no row focused, which is the state until j/k is pressed.
  const [cursor, setCursor] = useState(-1);
  const searchRef = useRef<HTMLInputElement>(null);
  const rowsRef = useRef<(HTMLAnchorElement | null)[]>([]);
  const navigate = useNavigate();

  const toggleSelected = (id: string) =>
    setSelected((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });
  // Scoped to the selected project — the subtitle prints these next to the list,
  // so an install-wide count here would contradict the rows on screen.
  const overview = useQuery({
    queryKey: ["dashboard", projectKey],
    queryFn: () => api.dashboard(projectKey),
    enabled: !!projectKey,
    retry: false,
  });
  // Paged: a project can have far more issues than one page (the API caps at 200 per
  // request), so "Load more" fetches the next offset and appends.
  const issues = useInfiniteQuery({
    queryKey: ["issues", projectKey, filter, sort],
    queryFn: ({ pageParam }) => api.listIssues(projectKey, filter, sort, PAGE_SIZE, pageParam),
    initialPageParam: 0,
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((n, p) => n + p.items.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    enabled: !!projectKey,
  });

  // Press "/" anywhere to jump to the filter box, and j/k/Enter/x to work the list
  // without leaving the keyboard — triage is a loop, and the mouse round trip is paid
  // on every issue in it.
  useShortcut((e) => {
    if (e.key === "/") {
      e.preventDefault();
      searchRef.current?.focus();
      return;
    }
    if (items.length === 0) return;

    switch (e.key) {
      case "j":
      case "ArrowDown":
        e.preventDefault();
        setCursor((c) => Math.min(c < 0 ? 0 : c + 1, items.length - 1));
        break;
      case "k":
      case "ArrowUp":
        e.preventDefault();
        setCursor((c) => Math.max(c <= 0 ? 0 : c - 1, 0));
        break;
      case "Enter":
        if (cursor >= 0) {
          e.preventDefault();
          navigate(`/issues/${items[cursor].key}`);
        }
        break;
      case "x":
        if (cursor >= 0) {
          e.preventDefault();
          toggleSelected(items[cursor].id);
        }
        break;
      case "Escape":
        setCursor(-1);
        break;
    }
  });

  // Keep the focused row on screen; a cursor you have to scroll to find is no help.
  useEffect(() => {
    if (cursor < 0) return;
    rowsRef.current[cursor]?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  // A new filter renders a different list, so the old position means nothing.
  useEffect(() => setCursor(-1), [filter, sort, projectKey]);

  // A project's default view is what the list opens with when no filter was asked for.
  // Only on arrival with no ?filter — never over an explicit link, and never twice.
  const projectViews = useQuery({
    queryKey: ["project-views", projectKey],
    queryFn: () => api.listProjectViews(projectKey),
    enabled: !!projectKey,
  });
  useEffect(() => {
    if (!projectKey || searchParams.get("filter") || appliedDefaultFor.current === projectKey) return;
    const items = projectViews.data?.items;
    if (!items) return;
    appliedDefaultFor.current = projectKey;
    const fallback = items.find((v) => v.is_default);
    if (fallback) {
      setFilter(fallback.query);
      if (fallback.sort) setSort(fallback.sort);
    }
  }, [projectKey, projectViews.data, searchParams]);

  // "c" navigates to /issues?new=1; when we are already here that is a param change,
  // not a mount, so the initial state above never runs.
  useEffect(() => {
    if (searchParams.get("new") === "1") setShowNewIssue(true);
  }, [searchParams]);

  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const hasProjects = projects.length > 0;
  const items = issues.data?.pages.flatMap((p) => p.items) ?? [];
  const total = issues.data?.pages[0]?.total ?? 0;
  // The rollup covers the whole filtered set, not the loaded pages — see the API.
  const effort = issues.data?.pages[0]?.effort;

  const subtitle =
    overview.data && projectKey
      ? `Project ${projectKey} · ${overview.data.open_issues} open · ${overview.data.critical_issues} critical`
      : projectKey
        ? `Project ${projectKey} · ${total} ${total === 1 ? "issue" : "issues"}`
        : "No project selected";

  return (
    <div>
      {/* Topbar */}
      <div className="sticky top-0 z-10 flex flex-wrap items-end justify-between gap-3 border-b border-hairline bg-paper/80 px-4 pb-5 pt-5 backdrop-blur md:px-9 md:pt-7">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-2xl font-bold leading-none tracking-[-0.02em] text-ink md:text-[30px]">Issues</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">{subtitle}</p>
        </div>
        <div className="flex w-full items-center gap-2 md:w-auto md:gap-3">
          <label className="flex h-10 min-w-0 grow items-center gap-2 rounded-md border border-hairline bg-paper px-3 focus-within:border-blueprint md:w-[280px] md:grow-0">
            <IconSearch size={16} className="text-graphite" />
            <input
              ref={searchRef}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search issues…"
              className="grow bg-transparent text-sm text-ink outline-none placeholder:text-graphite-soft"
            />
            <span className="rounded-sm border border-hairline px-1.5 py-px font-mono text-xs text-graphite-soft">/</span>
          </label>
          <button
            onClick={() => setShowNewIssue(true)}
            disabled={!projectKey}
            className="flex h-10 shrink-0 items-center gap-1.5 rounded-md bg-blueprint px-3 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50 md:px-4"
          >
            <IconPlus size={15} />
            <span className="hidden sm:inline">New issue</span>
          </button>
        </div>
      </div>

      {!hasProjects && !projects.length && (
        <div className="m-9 rounded-lg border border-hairline bg-panel p-6 text-sm text-graphite">
          No projects yet.{" "}
          {canManage
            ? "Create one from the project switcher in the sidebar."
            : "Ask an admin to create a project."}
        </div>
      )}

      {hasProjects && (
        <>
          {/* Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-hairline bg-paper/80 px-4 md:px-9 py-2.5 backdrop-blur">
            <div className="flex flex-wrap items-center gap-2">
              {QUICK_FILTERS.map((q) => {
                const active = filter === q.filter;
                return (
                  <button
                    key={q.label}
                    onClick={() => setFilter(q.filter)}
                    className={`flex h-[26px] items-center rounded-full px-3 text-[13px] transition ${
                      active
                        ? "bg-blueprint font-semibold text-paper"
                        : "border border-hairline font-medium text-graphite hover:border-graphite hover:text-ink"
                    }`}
                  >
                    {q.label}
                  </button>
                );
              })}
              <span className="mx-1 h-5 w-px bg-hairline" />
              <button
                onClick={() => {
                  setFilter((f) => (f ? `${f} label:` : "label:"));
                  searchRef.current?.focus();
                }}
                className="flex h-[26px] items-center gap-1.5 rounded-full border border-dashed border-hairline px-2.5 text-graphite-soft transition hover:border-graphite hover:text-graphite"
              >
                <IconLabelLines size={13} />
                <span className="font-mono text-xs">label:</span>
              </button>
              <SharedViews projectKey={projectKey} filter={filter} canManage={canManage} onApply={setFilter} />
              <SavedSearches filter={filter} projectKey={projectKey} canManage={canManage} onApply={setFilter} />
            </div>
            <div className="flex items-center gap-3 md:gap-4">
              <span className="font-mono text-xs text-graphite">
                {total} {total === 1 ? "issue" : "issues"}
              </span>
              <EffortSummary effort={effort} />
              <ExportMenu projectKey={projectKey} filter={filter} sort={sort} disabled={total === 0} />
              <SortSelect value={sort} onChange={setSort} />
            </div>
          </div>

          {/* Column header */}
          <div className="flex items-center gap-2 border-b border-hairline bg-panel px-4 py-2.5 sm:gap-4 md:px-9">
            <span className="flex w-4 shrink-0 justify-center">
              <input
                type="checkbox"
                aria-label="Select all"
                checked={items.length > 0 && selected.size === items.length}
                onChange={(e) =>
                  setSelected(e.target.checked ? new Set(items.map((i) => i.id)) : new Set())
                }
                className="accent-blueprint"
              />
            </span>
            <Lane className="w-3" />
            <Lane className="w-16 sm:w-[88px]">ID</Lane>
            <Lane className="grow">Issue</Lane>
            <Lane className="hidden w-10 text-center sm:block">Who</Lane>
            <Lane className="hidden w-11 sm:block">Pri</Lane>
            <Lane className="hidden w-[92px] lg:block">Severity</Lane>
            <Lane className="w-[88px] sm:w-[132px]">Status</Lane>
            <Lane className="hidden w-[70px] text-right md:block">Updated</Lane>
          </div>

          {/* Rows */}
          {issues.isLoading && <div className="px-4 md:px-9 py-8 text-sm text-graphite">Loading…</div>}
          {issues.isError && (
            <div className="px-4 md:px-9 py-8 text-sm text-critical">{(issues.error as Error).message}</div>
          )}
          {items.map((issue, index) => (
            <IssueRow
              key={issue.id}
              issue={issue}
              selected={selected.has(issue.id)}
              focused={index === cursor}
              rowRef={(el) => (rowsRef.current[index] = el)}
              onToggle={() => toggleSelected(issue.id)}
            />
          ))}
          {issues.isSuccess && items.length === 0 && (
            <div className="px-4 md:px-9 py-10 text-sm text-graphite-soft">No issues match this filter.</div>
          )}

          {/* Footer */}
          {items.length > 0 && (
            <div className="flex items-center justify-between px-4 md:px-9 py-4">
              <span className="font-mono text-xs text-graphite-soft">
                Showing {items.length} of {total} {total === 1 ? "issue" : "issues"}
              </span>
              {issues.hasNextPage && (
                <button
                  onClick={() => issues.fetchNextPage()}
                  disabled={issues.isFetchingNextPage}
                  className="flex items-center gap-1.5 font-semibold text-blueprint transition hover:opacity-80 disabled:opacity-50"
                >
                  {issues.isFetchingNextPage ? "Loading…" : "Load more"}
                  <IconArrowDown size={13} />
                </button>
              )}
            </div>
          )}
        </>
      )}

      {showNewIssue && projectKey && (
        <NewIssueForm
          projectKey={projectKey}
          onClose={() => {
            setShowNewIssue(false);
            // Drop ?new=1 so a reload (or Back) does not reopen the composer.
            if (searchParams.has("new")) {
              const next = new URLSearchParams(searchParams);
              next.delete("new");
              setSearchParams(next, { replace: true });
            }
          }}
        />
      )}

      {selected.size > 0 && (
        <BulkBar ids={[...selected]} projectKey={projectKey} onDone={() => setSelected(new Set())} />
      )}
    </div>
  );
}

function Lane({ className = "", children }: { className?: string; children?: React.ReactNode }) {
  return (
    <span className={`shrink-0 font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft ${className}`}>
      {children}
    </span>
  );
}

function IssueRow({
  issue,
  selected,
  focused,
  rowRef,
  onToggle,
}: {
  issue: Issue;
  selected: boolean;
  focused: boolean;
  rowRef: (el: HTMLAnchorElement | null) => void;
  onToggle: () => void;
}) {
  return (
    <Link
      ref={rowRef}
      to={`/issues/${issue.key}`}
      className={`flex items-center gap-2 border-b border-hairline px-4 py-2 transition hover:bg-panel/60 sm:gap-4 md:px-9 ${
        selected ? "bg-blueprint-soft/40" : ""
      } ${focused ? "bg-panel ring-1 ring-inset ring-blueprint" : ""}`}
    >
      <span
        className="flex w-4 shrink-0 justify-center"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onToggle();
        }}
      >
        <input type="checkbox" aria-label={`Select ${issue.key}`} checked={selected} readOnly className="accent-blueprint" />
      </span>
      <div className="flex w-3 shrink-0 justify-center">
        <SeverityBar severity={issue.severity} />
      </div>
      {/* whitespace-nowrap and a *minimum* width rather than a fixed one: a fixed
          88px wrapped "RECORDER-107" onto a second line, making every row in that
          project half again as tall. Long keys now nudge the title instead. */}
      <span className="shrink-0 whitespace-nowrap font-mono text-sm font-medium text-blueprint sm:min-w-[88px]">
        {issue.key}
      </span>
      {/* One line from lg up, two below it. The metadata used to sit on its own line
          always, which made every row ~60px: on a 44-issue project that is eight rows
          per screen where twelve fit. The title still gets whatever width is left. */}
      <div className="flex min-w-0 grow flex-col gap-1 lg:flex-row lg:items-center lg:gap-3">
        <span className="truncate text-[15px] font-medium leading-tight text-ink lg:min-w-0 lg:grow">
          {issue.title}
        </span>
        <div className="flex min-w-0 shrink-0 items-center gap-1.5 overflow-hidden">
          {/* Labels only where there is genuine slack. Sharing a line with the title
              they cost it ~120px, and the title is the only thing in the row you
              cannot read off another column. Below 2xl they stay on their own line. */}
          <span className="hidden min-w-0 items-center gap-1.5 sm:flex lg:hidden 2xl:flex">
            {issue.labels?.slice(0, 3).map((l) => <LabelChip key={l} name={l} />)}
          </span>
          {/* Deadline state sits on the row itself rather than in a column: it only
              applies to some issues, and an empty column on every other row costs
              more width than it is worth. */}
          <SLAPill sla={issue.sla} />
          <DueChip dueAt={issue.due_at} resolved={!!issue.resolved_at} />
          <EstimateChip minutes={issue.estimate_minutes} />
          <ChecklistChip progress={issue.checklist} />
        </div>
      </div>
      <div className="hidden w-10 shrink-0 justify-center sm:flex">
        <Avatar user={issue.assignee} size={28} />
      </div>
      <div className="hidden w-11 shrink-0 sm:block">
        <PriorityText priority={issue.priority} />
      </div>
      <div className="hidden w-[92px] shrink-0 lg:block">
        <SeverityMark severity={issue.severity} />
      </div>
      <div className="w-[88px] shrink-0 sm:w-[132px]">
        <StatusPill status={issue.status} />
      </div>
      <span className="hidden w-[70px] shrink-0 text-right font-mono text-xs text-graphite md:block">
        {shortAgo(issue.updated_at)}
      </span>
    </Link>
  );
}

/**
 * ExportMenu downloads the current filter's whole result set — not the page on screen.
 * A plain link, so the browser streams it to disk; fetching it into memory first would
 * undo the point of a streaming endpoint.
 */
function ExportMenu({
  projectKey,
  filter,
  sort,
  disabled,
}: {
  projectKey: string;
  filter: string;
  sort: string;
  disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  if (!projectKey) return null;

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        disabled={disabled}
        title={disabled ? "Nothing to export" : "Export these issues"}
        className="flex h-[30px] items-center gap-1.5 rounded-md border border-hairline px-3 text-sm text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-40"
      >
        <IconArrowDown size={13} />
        <span className="hidden sm:inline">Export</span>
      </button>
      {open && (
        <>
          <button className="fixed inset-0 z-10 cursor-default" aria-hidden onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-[34px] z-20 w-40 rounded-md border border-hairline bg-paper py-1 shadow-lg shadow-ink/5">
            {(["csv", "json"] as const).map((format) => (
              <a
                key={format}
                href={api.exportIssuesURL(projectKey, filter, sort, format)}
                onClick={() => setOpen(false)}
                className="block px-3 py-2 text-sm text-ink transition hover:bg-panel"
              >
                Download {format.toUpperCase()}
              </a>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

/**
 * EffortSummary states how much of the current queue is even estimated before it
 * states how big it is. "3w estimated" over a set where two issues out of forty carry
 * a number is not a plan, and the coverage is the part that says so.
 */
function EffortSummary({ effort }: { effort?: EffortRollup }) {
  if (!effort || effort.estimated === 0) return null;
  const partial = effort.estimated < effort.issues;
  // Both numbers in one unit — "2d est · 270m spent" makes the reader convert
  // before they can tell whether it is a problem.
  const fmt = sameUnit(effort.estimate_minutes, effort.spent_minutes);
  return (
    <span
      title={
        partial
          ? `${effort.estimated} of ${effort.issues} issues are estimated`
          : "every issue in this queue is estimated"
      }
      className="hidden font-mono text-xs text-graphite sm:inline"
    >
      {fmt(effort.estimate_minutes)} est
      {effort.spent_minutes > 0 && ` · ${fmt(effort.spent_minutes)} spent`}
      {partial && (
        <span className="text-graphite-soft">
          {" "}
          ({effort.estimated}/{effort.issues})
        </span>
      )}
    </span>
  );
}

function SortSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const label = SORTS.find((s) => s.value === value)?.label ?? "Newest";
  return (
    <div className="relative">
      <div className="flex h-[30px] items-center gap-1.5 rounded-md border border-hairline px-3">
        <span className="text-sm text-graphite">Sort</span>
        <span className="text-sm font-semibold text-ink">{label}</span>
        <span className="text-graphite">▾</span>
      </div>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Sort issues"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        {SORTS.map((s) => (
          <option key={s.value} value={s.value}>
            {s.label}
          </option>
        ))}
      </select>
    </div>
  );
}

// Saved searches: personal named filters rendered as chips next to the quick
// filters. Saving upserts by name; the × on an active chip deletes it.
/**
 * SharedViews renders a project's agreed queues. Visually distinct from personal ones —
 * a chip that everybody sees and only maintainers can change is a different kind of
 * thing from one you made for yourself, and they sit next to each other.
 */
function SharedViews({
  projectKey,
  filter,
  canManage,
  onApply,
}: {
  projectKey: string;
  filter: string;
  canManage: boolean;
  onApply: (f: string) => void;
}) {
  const qc = useQueryClient();
  const views = useQuery({
    queryKey: ["project-views", projectKey],
    queryFn: () => api.listProjectViews(projectKey),
    enabled: !!projectKey,
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["project-views", projectKey] });
  const del = useMutation({ mutationFn: (id: string) => api.deleteProjectView(id), onSuccess: invalidate });
  const setDefault = useMutation({
    mutationFn: (id: string) => api.updateProjectView(id, { is_default: true }),
    onSuccess: invalidate,
  });

  return (
    <>
      {(views.data?.items ?? []).map((v) => {
        const active = filter === v.query;
        return (
          <span key={v.id} className="group relative inline-flex">
            <button
              onClick={() => onApply(v.query)}
              title={`${v.query}${v.description ? ` — ${v.description}` : ""}${
                v.author ? ` (shared by ${v.author.display_name})` : ""
              }`}
              className={`flex h-[30px] items-center gap-1.5 rounded-full px-3.5 text-sm transition ${
                canManage ? "pr-6" : ""
              } ${
                active
                  ? "bg-blueprint font-semibold text-paper"
                  : "border border-blueprint/40 bg-blueprint-soft/40 font-medium text-blueprint hover:border-blueprint"
              }`}
            >
              {v.is_default && <span title="Opens by default">★</span>}
              {v.name}
            </button>
            {canManage && (
              <ViewMenu
                view={v}
                projectKey={projectKey}
                active={active}
                onSetDefault={() => setDefault.mutate(v.id)}
                onDelete={() => del.mutate(v.id)}
              />
            )}
          </span>
        );
      })}
    </>
  );
}

/**
 * ViewMenu is a real menu rather than stacked confirm() dialogs. Chaining two confirms
 * means cancelling "make this the default" immediately asks about deleting it, which is
 * how somebody loses a view they only meant to leave alone.
 */
function ViewMenu({
  view,
  projectKey,
  active,
  onSetDefault,
  onDelete,
}: {
  view: SavedSearch;
  projectKey: string;
  active: boolean;
  onSetDefault: () => void;
  onDelete: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-label={`Manage shared view ${view.name}`}
        className={`absolute right-2 top-1/2 -translate-y-1/2 text-xs transition ${
          open ? "opacity-100" : "opacity-0 group-hover:opacity-100"
        } ${active ? "text-paper" : "text-blueprint/70 hover:text-blueprint"}`}
      >
        ⋯
      </button>
      {open && (
        <>
          <button className="fixed inset-0 z-10 cursor-default" aria-hidden onClick={() => setOpen(false)} />
          <div className="absolute left-0 top-[34px] z-20 w-52 rounded-md border border-hairline bg-paper py-1 text-left shadow-lg shadow-ink/5">
            {!view.is_default && (
              <button
                onClick={() => {
                  setOpen(false);
                  onSetDefault();
                }}
                className="block w-full px-3 py-2 text-left text-sm text-ink transition hover:bg-panel"
              >
                Open by default in {projectKey}
              </button>
            )}
            <button
              onClick={() => {
                setOpen(false);
                if (window.confirm(`Delete the shared view “${view.name}”? Everyone on ${projectKey} loses it.`)) {
                  onDelete();
                }
              }}
              className="block w-full px-3 py-2 text-left text-sm text-critical transition hover:bg-critical-soft"
            >
              Delete for everyone
            </button>
          </div>
        </>
      )}
    </>
  );
}

function SavedSearches({
  filter,
  projectKey,
  canManage,
  onApply,
}: {
  filter: string;
  projectKey: string;
  canManage: boolean;
  onApply: (f: string) => void;
}) {
  const qc = useQueryClient();
  const saved = useQuery({ queryKey: ["saved-searches"], queryFn: () => api.listSavedSearches() });
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("");

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["saved-searches"] });
    qc.invalidateQueries({ queryKey: ["project-views", projectKey] });
  };
  // Promoting keeps the same row, so the author and creation date survive.
  const share = useMutation({ mutationFn: (id: string) => api.shareSavedSearch(id, projectKey), onSuccess: invalidate });
  const save = useMutation({
    mutationFn: () => api.saveSavedSearch(name.trim(), filter),
    onSuccess: () => {
      setNaming(false);
      setName("");
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteSavedSearch(id), onSuccess: invalidate });

  const items = saved.data?.items ?? [];

  return (
    <>
      {items.map((s) => {
        const active = filter === s.query;
        return (
          <span key={s.id} className="group relative inline-flex">
            <button
              onClick={() => onApply(s.query)}
              title={s.query}
              className={`flex h-[30px] items-center rounded-full px-3.5 pr-9 text-sm transition ${
                active
                  ? "bg-blueprint font-semibold text-paper"
                  : "border border-hairline font-medium text-graphite hover:border-graphite hover:text-ink"
              }`}
            >
              {s.name}
            </button>
            <button
              onClick={() => {
                if (window.confirm(`Delete saved search “${s.name}”?`)) del.mutate(s.id);
              }}
              aria-label={`Delete saved search ${s.name}`}
              className={`absolute right-2 top-1/2 -translate-y-1/2 text-xs opacity-0 transition group-hover:opacity-100 ${
                active ? "text-paper" : "text-graphite-soft hover:text-critical"
              }`}
            >
              ×
            </button>
            {canManage && projectKey && (
              <button
                onClick={() => {
                  if (window.confirm(`Share “${s.name}” with everyone on ${projectKey}? It becomes the project's view, not yours.`)) {
                    share.mutate(s.id);
                  }
                }}
                title={`Share with everyone on ${projectKey}`}
                aria-label={`Share saved search ${s.name} with the project`}
                className={`absolute right-5 top-1/2 -translate-y-1/2 text-xs opacity-0 transition group-hover:opacity-100 ${
                  active ? "text-paper" : "text-graphite-soft hover:text-blueprint"
                }`}
              >
                ↑
              </button>
            )}
          </span>
        );
      })}
      {naming ? (
        <span className="flex h-[30px] items-center gap-1 rounded-full border border-blueprint bg-paper px-2">
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && name.trim() && filter.trim()) save.mutate();
              else if (e.key === "Escape") setNaming(false);
            }}
            placeholder="Name this filter…"
            className="w-32 bg-transparent text-sm text-ink outline-none placeholder:text-graphite-soft"
          />
          <button
            disabled={!name.trim() || !filter.trim() || save.isPending}
            onClick={() => save.mutate()}
            className="text-xs font-semibold text-blueprint disabled:opacity-50"
          >
            Save
          </button>
        </span>
      ) : (
        <button
          onClick={() => setNaming(true)}
          disabled={!filter.trim()}
          title={filter.trim() ? "Save the current filter" : "Type a filter first"}
          className="flex h-[30px] items-center gap-1 rounded-full border border-dashed border-hairline px-3 font-mono text-xs text-graphite-soft transition hover:border-graphite hover:text-graphite disabled:opacity-40"
        >
          ☆ save
        </button>
      )}
    </>
  );
}

// Floating action bar shown while rows are selected. Every action calls the
// bulk endpoint, reports failures, refreshes the list, and clears selection.
function BulkBar({ ids, projectKey, onDone }: { ids: string[]; projectKey: string; onDone: () => void }) {
  const qc = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const { projects } = useProject();

  const run = useMutation({
    mutationFn: (body: Parameters<typeof api.bulkUpdateIssues>[0]) => api.bulkUpdateIssues(body),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["issues"] });
      qc.invalidateQueries({ queryKey: ["dashboard"] });
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
