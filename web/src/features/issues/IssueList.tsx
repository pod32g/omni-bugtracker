import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { api, type Issue, type IssueStatus, type Priority } from "../../lib/api";
import { useProject } from "../../lib/project";
import { timeAgo } from "../../lib/activity";
import { Avatar, LabelChip, PriorityText, SeverityBar, SeverityMark, StatusPill } from "../../components/Badges";
import { IconArrowDown, IconLabelLines, IconPlus, IconSearch } from "../../components/icons";
import { NewIssueForm } from "./NewIssueForm";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

const QUICK_FILTERS = [
  { label: "Open", filter: "is:open" },
  { label: "Assigned to me", filter: "assignee:@me" },
  { label: "Critical", filter: "severity:critical" },
  { label: "All", filter: "" },
];

const SORTS = [
  { value: "", label: "Newest" },
  { value: "created_at", label: "Oldest" },
  { value: "-updated_at", label: "Recently updated" },
  { value: "priority", label: "Priority" },
  { value: "severity", label: "Severity" },
];

// Compact relative time for the dense list ("2h", "5h", "1d", "just now").
const shortAgo = (iso: string) => timeAgo(iso).replace(" ago", "");

export function IssueList() {
  const { projects, projectKey } = useProject();
  const [searchParams] = useSearchParams();
  // Initial filter can be deep-linked from the dashboard gadgets (e.g. ?filter=assignee:@me).
  const [filter, setFilter] = useState(() => searchParams.get("filter") ?? "is:open");
  const [sort, setSort] = useState("");
  const [showNewIssue, setShowNewIssue] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const searchRef = useRef<HTMLInputElement>(null);

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });
  const overview = useQuery({ queryKey: ["dashboard"], queryFn: () => api.dashboard(), retry: false });
  const issues = useQuery({
    queryKey: ["issues", projectKey, filter, sort],
    queryFn: () => api.listIssues(projectKey, filter, sort),
    enabled: !!projectKey,
  });

  // Press "/" anywhere to jump to the filter box.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey) return;
      const el = document.activeElement;
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return;
      e.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const hasProjects = projects.length > 0;
  const items = issues.data?.items ?? [];
  const total = issues.data?.total ?? 0;

  const subtitle =
    overview.data && projectKey
      ? `Project ${projectKey} · ${overview.data.open_issues} open · ${overview.data.critical_issues} critical`
      : projectKey
        ? `Project ${projectKey} · ${total} ${total === 1 ? "issue" : "issues"}`
        : "No project selected";

  return (
    <div>
      {/* Topbar */}
      <div className="sticky top-0 z-10 flex items-end justify-between border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Issues</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">{subtitle}</p>
        </div>
        <div className="flex items-center gap-3">
          <label className="flex h-10 w-[280px] items-center gap-2 rounded-md border border-hairline bg-paper px-3 focus-within:border-blueprint">
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
            className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            <IconPlus size={15} />
            New issue
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
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-hairline bg-paper/80 px-9 py-4 backdrop-blur">
            <div className="flex flex-wrap items-center gap-2">
              {QUICK_FILTERS.map((q) => {
                const active = filter === q.filter;
                return (
                  <button
                    key={q.label}
                    onClick={() => setFilter(q.filter)}
                    className={`flex h-[30px] items-center rounded-full px-3.5 text-sm transition ${
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
                className="flex h-[30px] items-center gap-1.5 rounded-full border border-dashed border-hairline px-3 text-graphite-soft transition hover:border-graphite hover:text-graphite"
              >
                <IconLabelLines size={13} />
                <span className="font-mono text-xs">label:</span>
              </button>
              <SavedSearches filter={filter} onApply={setFilter} />
            </div>
            <div className="flex items-center gap-4">
              <span className="font-mono text-xs text-graphite">
                {total} {total === 1 ? "issue" : "issues"}
              </span>
              <SortSelect value={sort} onChange={setSort} />
            </div>
          </div>

          {/* Column header */}
          <div className="flex items-center gap-4 border-b border-hairline bg-panel px-9 py-2.5">
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
            <Lane className="w-[88px]">ID</Lane>
            <Lane className="grow">Issue</Lane>
            <Lane className="w-10 text-center">Who</Lane>
            <Lane className="w-11">Pri</Lane>
            <Lane className="w-[92px]">Severity</Lane>
            <Lane className="w-[132px]">Status</Lane>
            <Lane className="w-[70px] text-right">Updated</Lane>
          </div>

          {/* Rows */}
          {issues.isLoading && <div className="px-9 py-8 text-sm text-graphite">Loading…</div>}
          {issues.isError && (
            <div className="px-9 py-8 text-sm text-critical">{(issues.error as Error).message}</div>
          )}
          {items.map((issue) => (
            <IssueRow
              key={issue.id}
              issue={issue}
              selected={selected.has(issue.id)}
              onToggle={() =>
                setSelected((s) => {
                  const next = new Set(s);
                  if (next.has(issue.id)) next.delete(issue.id);
                  else next.add(issue.id);
                  return next;
                })
              }
            />
          ))}
          {issues.isSuccess && items.length === 0 && (
            <div className="px-9 py-10 text-sm text-graphite-soft">No issues match this filter.</div>
          )}

          {/* Footer */}
          {items.length > 0 && (
            <div className="flex items-center justify-between px-9 py-4">
              <span className="font-mono text-xs text-graphite-soft">
                Showing {items.length} of {total} {total === 1 ? "issue" : "issues"}
              </span>
              {items.length < total && (
                <span className="flex items-center gap-1.5 font-semibold text-blueprint">
                  Load more
                  <IconArrowDown size={13} />
                </span>
              )}
            </div>
          )}
        </>
      )}

      {showNewIssue && projectKey && (
        <NewIssueForm projectKey={projectKey} onClose={() => setShowNewIssue(false)} />
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

function IssueRow({ issue, selected, onToggle }: { issue: Issue; selected: boolean; onToggle: () => void }) {
  return (
    <Link
      to={`/issues/${issue.key}`}
      className={`flex items-center gap-4 border-b border-hairline px-9 py-[11px] transition hover:bg-panel/60 ${
        selected ? "bg-blueprint-soft/40" : ""
      }`}
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
      <span className="w-[88px] shrink-0 font-mono text-sm font-medium text-blueprint">{issue.key}</span>
      <div className="flex min-w-0 grow flex-col gap-1">
        <span className="truncate text-[15px] font-medium leading-tight text-ink">{issue.title}</span>
        <div className="flex items-center gap-1.5">
          {issue.labels?.slice(0, 3).map((l) => <LabelChip key={l} name={l} />)}
          <span className="font-mono text-xs text-graphite-soft">#{issue.number}</span>
        </div>
      </div>
      <div className="flex w-10 shrink-0 justify-center">
        <Avatar user={issue.assignee} size={28} />
      </div>
      <div className="w-11 shrink-0">
        <PriorityText priority={issue.priority} />
      </div>
      <div className="w-[92px] shrink-0">
        <SeverityMark severity={issue.severity} />
      </div>
      <div className="w-[132px] shrink-0">
        <StatusPill status={issue.status} />
      </div>
      <span className="w-[70px] shrink-0 text-right font-mono text-xs text-graphite">{shortAgo(issue.updated_at)}</span>
    </Link>
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
function SavedSearches({ filter, onApply }: { filter: string; onApply: (f: string) => void }) {
  const qc = useQueryClient();
  const saved = useQuery({ queryKey: ["saved-searches"], queryFn: () => api.listSavedSearches() });
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["saved-searches"] });
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
              className={`flex h-[30px] items-center rounded-full px-3.5 pr-6 text-sm transition ${
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
        window.alert(`${res.updated} updated, ${res.failed.length} failed:\n` +
          res.failed.map((f) => `${f.key}: ${f.error}`).join("\n"));
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

      {run.isPending && <span className="font-mono text-xs text-graphite-soft">Applying…</span>}
      <button onClick={onDone} className="text-sm text-graphite transition hover:text-ink">
        Clear
      </button>
    </div>
  );
}
