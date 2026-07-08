import { useQuery } from "@tanstack/react-query";
import { api, type DashboardOverview } from "../../lib/api";

const empty: DashboardOverview = {
  open_issues: 0,
  critical_issues: 0,
  avg_resolution_hours: 0,
  mttr_hours: 0,
  regression_rate: 0,
  issues_by_component: {},
  issues_by_release: {},
  team_workload: {},
  recent_activity: [],
};

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
      <div className="text-sm text-slate-400">{label}</div>
      <div className={`mt-2 text-3xl font-semibold ${accent ? "text-accent-hover" : ""}`}>
        {value}
      </div>
    </div>
  );
}

export function Dashboard() {
  const { data = empty, isError } = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.dashboard(),
  });

  return (
    <div>
      <h1 className="mb-1 text-2xl font-semibold">Dashboard</h1>
      <p className="mb-6 text-sm text-slate-400">
        {isError ? "Backend dashboard endpoint pending — showing placeholders." : "Project health at a glance."}
      </p>
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Stat label="Open Issues" value={String(data.open_issues)} />
        <Stat label="Critical" value={String(data.critical_issues)} accent />
        <Stat label="Avg Resolution" value={`${data.avg_resolution_hours.toFixed(1)}h`} />
        <Stat label="MTTR" value={`${data.mttr_hours.toFixed(1)}h`} />
      </div>
      <div className="mt-8 grid gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
          <h2 className="mb-3 text-sm font-medium text-slate-300">Issues by Component</h2>
          <BarList data={data.issues_by_component} />
        </div>
        <div className="rounded-xl border border-surface-border bg-surface-raised p-5">
          <h2 className="mb-3 text-sm font-medium text-slate-300">Team Workload</h2>
          <BarList data={data.team_workload} />
        </div>
      </div>
    </div>
  );
}

function BarList({ data }: { data: Record<string, number> }) {
  const entries = Object.entries(data);
  if (entries.length === 0) return <p className="text-sm text-slate-500">No data yet.</p>;
  const max = Math.max(...entries.map(([, v]) => v));
  return (
    <div className="space-y-2">
      {entries.map(([label, value]) => (
        <div key={label} className="flex items-center gap-3 text-sm">
          <span className="w-32 truncate text-slate-400">{label}</span>
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-white/5">
            <div className="h-full rounded-full bg-accent" style={{ width: `${(value / max) * 100}%` }} />
          </div>
          <span className="w-8 text-right tabular-nums text-slate-400">{value}</span>
        </div>
      ))}
    </div>
  );
}
