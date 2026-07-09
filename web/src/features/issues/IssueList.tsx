import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import { PriorityBadge, SeverityBadge, StatusBadge } from "../../components/Badges";
import { NewIssueForm } from "./NewIssueForm";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function IssueList() {
  const [filter, setFilter] = useState("is:open");
  const [project, setProject] = useState<string>("");
  const [showNewIssue, setShowNewIssue] = useState(false);
  const [showNewProject, setShowNewProject] = useState(false);

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });

  // Default to the first project once loaded.
  useEffect(() => {
    if (!project && projects.data?.items?.length) setProject(projects.data.items[0].key);
  }, [project, projects.data]);

  const issues = useQuery({
    queryKey: ["issues", project, filter],
    queryFn: () => api.listIssues(project, filter),
    enabled: !!project,
  });

  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const hasProjects = (projects.data?.items?.length ?? 0) > 0;

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">Issues</h1>
        {hasProjects && (
          <select
            value={project}
            onChange={(e) => setProject(e.target.value)}
            className="rounded-lg border border-surface-border bg-surface-raised px-3 py-1.5 text-sm outline-none focus:border-accent"
          >
            {projects.data!.items.map((p) => (
              <option key={p.key} value={p.key}>{p.key} — {p.name}</option>
            ))}
          </select>
        )}
        <span className="text-sm text-slate-400">{issues.data?.total ?? 0} total</span>
        <div className="ml-auto flex gap-2">
          {canManage && (
            <button
              onClick={() => setShowNewProject((s) => !s)}
              className="rounded-lg border border-surface-border px-3 py-1.5 text-sm text-slate-300 hover:border-accent hover:text-accent-hover"
            >
              New project
            </button>
          )}
          <button
            onClick={() => setShowNewIssue(true)}
            disabled={!project}
            className="rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-50"
          >
            New issue
          </button>
        </div>
      </div>

      {showNewProject && canManage && (
        <NewProjectInline
          onDone={(key) => {
            setShowNewProject(false);
            if (key) setProject(key);
          }}
        />
      )}

      {!hasProjects && !projects.isLoading && (
        <div className="rounded-xl border border-surface-border bg-surface-raised p-6 text-sm text-slate-400">
          No projects yet.{" "}
          {canManage ? "Create one with “New project” above." : "Ask an admin to create a project."}
        </div>
      )}

      {hasProjects && (
        <>
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter: is:open assignee:@me severity:critical type:bug"
            className="mb-4 w-full rounded-lg border border-surface-border bg-surface-raised px-3 py-2 font-mono text-sm outline-none focus:border-accent"
          />
          <div className="overflow-hidden rounded-xl border border-surface-border">
            {issues.isLoading && <div className="p-6 text-sm text-slate-400">Loading…</div>}
            {issues.isError && (
              <div className="p-6 text-sm text-severity-high">{(issues.error as Error).message}</div>
            )}
            {issues.data?.items?.map((issue) => (
              <Link
                key={issue.id}
                to={`/issues/${issue.key}`}
                className="flex items-center gap-3 border-b border-surface-border px-4 py-3 last:border-0 hover:bg-white/5"
              >
                <span className="w-24 font-mono text-sm text-accent-hover">{issue.key}</span>
                <span className="flex-1 truncate">{issue.title}</span>
                <SeverityBadge severity={issue.severity} />
                <PriorityBadge priority={issue.priority} />
                <StatusBadge status={issue.status} />
              </Link>
            ))}
            {issues.data?.items?.length === 0 && (
              <div className="p-6 text-sm text-slate-500">No issues match this filter.</div>
            )}
          </div>
        </>
      )}

      {showNewIssue && project && (
        <NewIssueForm projectKey={project} onClose={() => setShowNewIssue(false)} />
      )}
    </div>
  );
}

function NewProjectInline({ onDone }: { onDone: (key?: string) => void }) {
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => api.createProject({ key: key.toUpperCase(), name }),
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      onDone(p.key);
    },
  });
  return (
    <div className="mb-4 flex flex-wrap items-end gap-3 rounded-xl border border-surface-border bg-surface-raised p-4">
      <label className="text-sm">
        <span className="mb-1 block text-xs uppercase tracking-wide text-slate-500">Key</span>
        <input
          value={key}
          onChange={(e) => setKey(e.target.value.toUpperCase())}
          placeholder="BUG"
          maxLength={10}
          className="w-28 rounded-lg border border-surface-border bg-surface px-3 py-1.5 font-mono uppercase outline-none focus:border-accent"
        />
      </label>
      <label className="flex-1 text-sm">
        <span className="mb-1 block text-xs uppercase tracking-wide text-slate-500">Name</span>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Bug Tracker"
          className="w-full rounded-lg border border-surface-border bg-surface px-3 py-1.5 outline-none focus:border-accent"
        />
      </label>
      <button
        disabled={!/^[A-Z][A-Z0-9]{1,9}$/.test(key) || !name.trim() || create.isPending}
        onClick={() => create.mutate()}
        className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-50"
      >
        Create
      </button>
      <button onClick={() => onDone()} className="px-2 py-1.5 text-sm text-slate-400 hover:text-slate-200">
        Cancel
      </button>
      {create.isError && <p className="w-full text-sm text-severity-high">{(create.error as Error).message}</p>}
    </div>
  );
}
