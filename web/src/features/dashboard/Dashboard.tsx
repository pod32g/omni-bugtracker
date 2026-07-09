import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type DashboardOverview } from "../../lib/api";
import { humanizeVerb, timeAgo } from "../../lib/activity";

const empty: DashboardOverview = {
  open_issues: 0,
  critical_issues: 0,
  avg_resolution_hours: 0,
  mttr_hours: 0,
  regression_rate: 0,
  issues_by_status: {},
  issues_by_component: {},
  team_workload: {},
  recent_activity: [],
};

const STATUS_ORDER = ["open", "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened"];

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
      <div className="text-sm text-slate-400">{label}</div>
      <div className={`mt-2 text-3xl font-semibold ${accent ? "text-accent-hover" : ""}`}>{value}</div>
    </div>
  );
}

export function Dashboard() {
  const { data = empty, isError, isLoading } = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.dashboard(),
  });

  const hours = (h: number) => (h > 0 ? `${h.toFixed(1)}h` : "—");

  return (
    <div>
      <h1 className="mb-1 text-2xl font-semibold">Dashboard</h1>
      <p className="mb-6 text-sm text-slate-400">
        {isLoading ? "Loading…" : isError ? "Could not load dashboard." : "Project health at a glance."}
      </p>

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Stat label="Open Issues" value={String(data.open_issues)} />
        <Stat label="Critical (open)" value={String(data.critical_issues)} accent />
        <Stat label="Avg Resolution" value={hours(data.avg_resolution_hours)} />
        <Stat label="MTTR (30d)" value={hours(data.mttr_hours)} />
      </div>

      <div className="mt-8 grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
          <h2 className="mb-3 text-sm font-medium text-slate-300">Issues by Status</h2>
          <BarList
            data={data.issues_by_status}
            order={STATUS_ORDER}
            format={(k) => k.replace(/_/g, " ")}
          />
          <div className="mt-4 border-t border-surface-border pt-3 text-xs text-slate-500">
            Regression rate {(data.regression_rate * 100).toFixed(0)}%
          </div>
        </div>

        <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
          <h2 className="mb-3 text-sm font-medium text-slate-300">Team Workload</h2>
          <BarList data={data.team_workload} />
        </div>

        <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
          <h2 className="mb-3 text-sm font-medium text-slate-300">Recent Activity</h2>
          {data.recent_activity.length === 0 ? (
            <p className="text-sm text-slate-500">No activity yet.</p>
          ) : (
            <ul className="space-y-2 text-sm">
              {data.recent_activity.map((a) => (
                <li key={a.id} className="flex items-baseline gap-2">
                  <span className="text-slate-300">{a.actor?.display_name ?? "system"}</span>
                  <span className="text-slate-500">{humanizeVerb(a.verb)}</span>
                  {a.issue_key && (
                    <Link to={`/issues/${a.issue_key}`} className="font-mono text-xs text-accent-hover hover:underline">
                      {a.issue_key}
                    </Link>
                  )}
                  <span className="ml-auto shrink-0 text-xs text-slate-600">{timeAgo(a.occurred_at)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}

function BarList({
  data,
  order,
  format = (k) => k,
}: {
  data: Record<string, number>;
  order?: string[];
  format?: (k: string) => string;
}) {
  let entries = Object.entries(data);
  if (order) {
    entries = entries.sort((a, b) => order.indexOf(a[0]) - order.indexOf(b[0]));
  } else {
    entries = entries.sort((a, b) => b[1] - a[1]);
  }
  if (entries.length === 0) return <p className="text-sm text-slate-500">No data yet.</p>;
  const max = Math.max(...entries.map(([, v]) => v));
  return (
    <div className="space-y-2">
      {entries.map(([label, value]) => (
        <div key={label} className="flex items-center gap-3 text-sm">
          <span className="w-32 truncate capitalize text-slate-400">{format(label)}</span>
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-white/5">
            <div className="h-full rounded-full bg-accent" style={{ width: `${(value / max) * 100}%` }} />
          </div>
          <span className="w-8 text-right tabular-nums text-slate-400">{value}</span>
        </div>
      ))}
    </div>
  );
}
