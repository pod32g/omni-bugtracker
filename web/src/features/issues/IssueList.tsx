import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import { PriorityBadge, SeverityBadge, StatusBadge } from "../../components/Badges";

// A single default project keeps the scaffold simple; a project switcher slots in here.
const DEFAULT_PROJECT = "BUG";

export function IssueList() {
  const [filter, setFilter] = useState("is:open");
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["issues", DEFAULT_PROJECT, filter],
    queryFn: () => api.listIssues(DEFAULT_PROJECT, filter),
  });

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Issues</h1>
        <span className="text-sm text-slate-400">{data?.total ?? 0} total</span>
      </div>

      <input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Filter: is:open assignee:@me severity:critical component:auth"
        className="mb-4 w-full rounded-lg border border-surface-border bg-surface-raised px-3 py-2 font-mono text-sm outline-none focus:border-accent"
      />

      <div className="overflow-hidden rounded-xl border border-surface-border">
        {isLoading && <div className="p-6 text-sm text-slate-400">Loading…</div>}
        {isError && (
          <div className="p-6 text-sm text-severity-high">
            {(error as Error).message} — is the API running and a token set?
          </div>
        )}
        {data?.items?.map((issue) => (
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
        {data?.items?.length === 0 && (
          <div className="p-6 text-sm text-slate-500">No issues match this filter.</div>
        )}
      </div>
    </div>
  );
}
