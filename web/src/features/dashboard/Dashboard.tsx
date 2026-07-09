import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type DashboardOverview } from "../../lib/api";
import { useProject } from "../../lib/project";
import { humanizeVerb, timeAgo } from "../../lib/activity";
import { Avatar } from "../../components/Badges";
import type { ReactNode } from "react";

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

// Bar color per lifecycle state, matching the status palette.
const barColor: Record<string, string> = {
  open: "bg-blueprint",
  in_progress: "bg-high",
  blocked: "bg-critical",
  ready_for_review: "bg-blueprint",
  resolved: "bg-resolved",
  closed: "bg-graphite-soft",
  reopened: "bg-blueprint",
};

function activityDot(verb: string): string {
  if (verb.startsWith("comment")) return "bg-blueprint";
  if (verb.includes("resolved") || verb.includes("closed")) return "bg-resolved";
  if (verb.includes("created")) return "bg-critical";
  return "bg-high";
}

export function Dashboard() {
  const { projectKey } = useProject();
  const { data = empty, isError, isLoading } = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.dashboard(),
  });

  const hours = (h: number) => (h > 0 ? `${h.toFixed(1)}h` : "—");
  const pct = (n: number) => `${(n * 100).toFixed(0)}%`;

  return (
    <div>
      {/* Topbar */}
      <div className="sticky top-0 z-10 flex items-end justify-between border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Dashboard</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {isLoading
              ? "Loading…"
              : isError
                ? "Could not load dashboard"
                : `Project ${projectKey || "—"} · health overview`}
          </p>
        </div>
        <RangeControl />
      </div>

      <div className="flex max-w-5xl flex-col gap-4 px-9 py-8">
        {/* KPIs */}
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <Kpi
            label="Open Issues"
            value={String(data.open_issues)}
            foot={<span className="text-graphite">{data.critical_issues} critical open</span>}
          />
          <Kpi
            label="Critical · Open"
            value={String(data.critical_issues)}
            tone="critical"
            foot={<span className="text-critical">{data.critical_issues > 0 ? "needs triage" : "all clear"}</span>}
          />
          <Kpi
            label="Avg Resolution"
            value={hours(data.avg_resolution_hours)}
            foot={<span className="text-graphite">mean time to resolve</span>}
          />
          <Kpi
            label="MTTR · 30d"
            value={hours(data.mttr_hours)}
            foot={<span className="text-graphite">regression rate {pct(data.regression_rate)}</span>}
          />
        </div>

        {/* Charts */}
        <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
          <Card title="Issues by status">
            <BarList
              entries={orderedEntries(data.issues_by_status, STATUS_ORDER)}
              renderLabel={(k) => <span className="capitalize">{k.replace(/_/g, " ")}</span>}
              color={(k) => barColor[k] ?? "bg-blueprint"}
              labelWidth="w-24"
            />
          </Card>
          <Card title="Team workload · open">
            <BarList
              entries={sortedEntries(data.team_workload)}
              renderLabel={(name) => (
                <span className="flex items-center gap-2.5">
                  <Avatar user={{ id: name, email: name, display_name: name }} size={24} />
                  <span className="w-16 truncate text-ink">{name}</span>
                </span>
              )}
              color={() => "bg-blueprint"}
            />
          </Card>
        </div>

        {/* Activity */}
        <Card title="Recent activity">
          {data.recent_activity.length === 0 ? (
            <p className="text-sm text-graphite-soft">No activity yet.</p>
          ) : (
            <ul className="flex flex-col gap-3">
              {data.recent_activity.map((a) => (
                <li key={a.id} className="flex items-center gap-3">
                  <span className={`h-[7px] w-[7px] shrink-0 rounded-full ${activityDot(a.verb)}`} />
                  <span className="grow text-sm text-graphite">
                    <span className="text-ink">{a.actor?.display_name ?? "system"}</span> {humanizeVerb(a.verb)}
                    {a.issue_key && (
                      <>
                        {" "}
                        <Link to={`/issues/${a.issue_key}`} className="font-mono text-blueprint hover:underline">
                          {a.issue_key}
                        </Link>
                      </>
                    )}
                  </span>
                  <span className="shrink-0 font-mono text-xs text-graphite-soft">
                    {timeAgo(a.occurred_at).replace(" ago", "")}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}

function Kpi({
  label,
  value,
  foot,
  tone,
}: {
  label: string;
  value: string;
  foot?: ReactNode;
  tone?: "critical";
}) {
  const critical = tone === "critical";
  return (
    <div
      className={`flex flex-col gap-3.5 rounded-lg border p-5 ${
        critical ? "border-critical-border bg-critical-soft" : "border-hairline bg-paper"
      }`}
    >
      <span
        className={`font-mono text-[10px] font-medium uppercase tracking-caps ${
          critical ? "text-critical" : "text-graphite-soft"
        }`}
      >
        {label}
      </span>
      <span className={`text-[42px] font-bold leading-[0.9] tracking-[-0.02em] ${critical ? "text-critical" : "text-ink"}`}>
        {value}
      </span>
      {foot && <span className="font-mono text-xs">{foot}</span>}
    </div>
  );
}

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-5">
      <h2 className="text-sm font-semibold text-ink">{title}</h2>
      {children}
    </div>
  );
}

function BarList({
  entries,
  renderLabel,
  color,
  labelWidth,
}: {
  entries: [string, number][];
  renderLabel: (key: string) => ReactNode;
  color: (key: string) => string;
  labelWidth?: string;
}) {
  if (entries.length === 0) return <p className="text-sm text-graphite-soft">No data yet.</p>;
  const max = Math.max(...entries.map(([, v]) => v), 1);
  return (
    <div className="flex flex-col gap-3">
      {entries.map(([key, value]) => (
        <div key={key} className="flex items-center gap-3.5">
          <span className={`shrink-0 text-sm text-graphite ${labelWidth ?? ""}`}>{renderLabel(key)}</span>
          <div className="h-2 grow overflow-hidden rounded-[4px] bg-panel">
            <div className={`h-full rounded-[4px] ${color(key)}`} style={{ width: `${Math.round((value / max) * 100)}%` }} />
          </div>
          <span className="w-6 shrink-0 text-right font-mono text-sm font-semibold text-ink">{value}</span>
        </div>
      ))}
    </div>
  );
}

function orderedEntries(data: Record<string, number>, order: string[]): [string, number][] {
  return Object.entries(data).sort((a, b) => order.indexOf(a[0]) - order.indexOf(b[0]));
}

function sortedEntries(data: Record<string, number>): [string, number][] {
  return Object.entries(data).sort((a, b) => b[1] - a[1]);
}

function RangeControl() {
  return (
    <div className="flex items-center gap-0.5 rounded-md border border-hairline bg-panel p-0.5">
      {["7d", "30d", "90d"].map((r) =>
        r === "30d" ? (
          <span key={r} className="rounded-[7px] border border-hairline bg-paper px-3 py-1.5 text-sm font-semibold text-ink">
            {r}
          </span>
        ) : (
          <span
            key={r}
            title="Range selection coming soon"
            className="cursor-default rounded-[7px] px-3 py-1.5 text-sm text-graphite/60"
          >
            {r}
          </span>
        ),
      )}
    </div>
  );
}
