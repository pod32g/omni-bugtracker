import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type BurndownPoint, type Iteration, type Velocity } from "../../lib/api";
import { useProject } from "../../lib/project";
import { formatMinutes, sameUnit } from "../../lib/duration";
import { IconPlus } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

const stateTone: Record<string, string> = {
  active: "border-blueprint-border bg-blueprint-soft text-blueprint",
  planned: "border-hairline bg-panel text-graphite",
  completed: "border-resolved-border bg-resolved-soft text-resolved",
};

export function Iterations() {
  const { projectKey } = useProject();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);

  const iterations = useQuery({
    queryKey: ["iterations", projectKey],
    queryFn: () => api.listIterations(projectKey),
    enabled: !!projectKey,
  });
  const velocity = useQuery({
    queryKey: ["velocity", projectKey],
    queryFn: () => api.velocity(projectKey),
    enabled: !!projectKey,
  });

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["iterations", projectKey] });
    qc.invalidateQueries({ queryKey: ["velocity", projectKey] });
    qc.invalidateQueries({ queryKey: ["issues"] });
  };

  const items = iterations.data?.items ?? [];
  const active = items.find((i) => i.state === "active");

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-hairline px-4 md:px-9 py-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-ink">Iterations</h1>
          <p className="font-mono text-xs uppercase tracking-caps text-graphite-soft">
            {iterations.isLoading
              ? "Loading…"
              : `Project ${projectKey || "—"} · ${items.length} iteration${items.length === 1 ? "" : "s"}`}
          </p>
        </div>
        {canManage && (
          <button
            onClick={() => setCreating((c) => !c)}
            className="flex h-9 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90"
          >
            <IconPlus size={15} />
            {creating ? "Cancel" : "New iteration"}
          </button>
        )}
      </div>

      <div className="flex max-w-5xl flex-col gap-6 px-4 md:px-9 py-8">
        {creating && (
          <NewIterationForm
            projectKey={projectKey}
            onDone={() => {
              setCreating(false);
              invalidate();
            }}
          />
        )}

        {active && <Burndown iteration={active} />}

        <VelocityPanel
          history={velocity.data?.items ?? []}
          averageIssues={velocity.data?.average_issues ?? 0}
          averageMinutes={velocity.data?.average_minutes ?? 0}
          averagedOver={velocity.data?.averaged_over ?? 0}
          windowSize={velocity.data?.average_window_size ?? 0}
        />

        <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-lg border border-hairline bg-paper">
          {iterations.isLoading && <div className="p-5 text-sm text-graphite">Loading…</div>}
          {iterations.isSuccess && items.length === 0 && (
            <div className="p-5 text-sm text-graphite-soft">
              No iterations yet. An iteration is a time-boxed slice of work — milestones say
              what, iterations say when.
            </div>
          )}
          {items.map((it) => (
            <IterationRow
              key={it.id}
              iteration={it}
              others={items.filter((o) => o.id !== it.id && o.state !== "completed")}
              canManage={canManage}
              onChanged={invalidate}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function NewIterationForm({ projectKey, onDone }: { projectKey: string; onDone: () => void }) {
  const today = new Date();
  const inTwoWeeks = new Date(today.getTime() + 13 * 86_400_000);
  const [name, setName] = useState("");
  const [startsOn, setStartsOn] = useState(today.toLocaleDateString("en-CA"));
  const [endsOn, setEndsOn] = useState(inTwoWeeks.toLocaleDateString("en-CA"));
  const [goal, setGoal] = useState("");

  const create = useMutation({
    mutationFn: () => api.createIteration(projectKey, { name: name.trim(), starts_on: startsOn, ends_on: endsOn, goal }),
    onSuccess: onDone,
  });

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-hairline bg-paper p-5">
      <div className="flex flex-wrap items-end gap-3">
        <Labelled label="Name" className="grow">
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="2026-W32"
            className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
          />
        </Labelled>
        <Labelled label="Starts">
          <input
            type="date"
            value={startsOn}
            onChange={(e) => setStartsOn(e.target.value)}
            className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
          />
        </Labelled>
        <Labelled label="Ends">
          <input
            type="date"
            value={endsOn}
            onChange={(e) => setEndsOn(e.target.value)}
            className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
          />
        </Labelled>
        <button
          disabled={!name.trim() || create.isPending}
          onClick={() => create.mutate()}
          className="h-9 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          Create
        </button>
      </div>
      <Labelled label="Goal (optional)">
        <input
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          placeholder="What this iteration is for"
          className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
        />
      </Labelled>
      {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}
    </section>
  );
}

function Labelled({
  label,
  children,
  className = "",
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <label className={`text-sm ${className}`}>
      <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
        {label}
      </span>
      {children}
    </label>
  );
}

function IterationRow({
  iteration: it,
  others,
  canManage,
  onChanged,
}: {
  iteration: Iteration;
  others: Iteration[];
  canManage: boolean;
  onChanged: () => void;
}) {
  const [carrying, setCarrying] = useState(false);
  const update = useMutation({
    mutationFn: (state: Iteration["state"]) => api.updateIteration(it.id, { state }),
    onSuccess: onChanged,
  });
  const carry = useMutation({
    mutationFn: (to: string) => api.carryOverIteration(it.id, to),
    onSuccess: () => {
      setCarrying(false);
      onChanged();
    },
  });
  const del = useMutation({ mutationFn: () => api.deleteIteration(it.id), onSuccess: onChanged });

  const open = it.issues - it.done_issues;
  const pct = it.issues === 0 ? 0 : Math.round((it.done_issues / it.issues) * 100);

  return (
    <div className="flex flex-col gap-3 p-5">
      <div className="flex flex-wrap items-center gap-3">
        <Link
          to={`/issues?filter=${encodeURIComponent(`iteration:"${it.name}"`)}`}
          className="text-base font-semibold text-ink transition hover:text-blueprint"
        >
          {it.name}
        </Link>
        <span
          className={`rounded-full border px-2 py-px text-xs font-semibold capitalize ${stateTone[it.state]}`}
        >
          {it.state}
        </span>
        <span className="font-mono text-xs text-graphite-soft">
          {it.starts_on} → {it.ends_on}
        </span>
        {canManage && (
          <span className="ml-auto flex items-center gap-3 text-xs font-semibold">
            {it.state !== "active" && (
              <button onClick={() => update.mutate("active")} className="text-blueprint transition hover:opacity-80">
                Start
              </button>
            )}
            {it.state === "active" && (
              <button onClick={() => update.mutate("completed")} className="text-blueprint transition hover:opacity-80">
                Complete
              </button>
            )}
            {open > 0 && (
              <button onClick={() => setCarrying((c) => !c)} className="text-blueprint transition hover:opacity-80">
                {carrying ? "Cancel" : `Carry over ${open}`}
              </button>
            )}
            <button
              onClick={() => {
                if (window.confirm(`Delete ${it.name}? Its issues return to the backlog.`)) del.mutate();
              }}
              className="text-graphite transition hover:text-critical"
            >
              Delete
            </button>
          </span>
        )}
      </div>

      {it.goal && <p className="text-sm text-graphite">{it.goal}</p>}

      <div className="flex flex-wrap items-center gap-3">
        <div className="h-1.5 min-w-[140px] grow overflow-hidden rounded-full bg-panel">
          <div className="h-full rounded-full bg-resolved transition-all" style={{ width: `${pct}%` }} />
        </div>
        <span className="shrink-0 font-mono text-xs text-graphite-soft">
          {it.done_issues}/{it.issues} done · {pct}%
        </span>
        {it.effort.estimated > 0 && (
          <span className="shrink-0 font-mono text-xs text-graphite-soft">
            {formatMinutes(it.effort.remaining_minutes)} left of{" "}
            {formatMinutes(it.effort.estimate_minutes)}
            {it.effort.estimated < it.issues && ` (${it.effort.estimated}/${it.issues} estimated)`}
          </span>
        )}
      </div>

      {carrying && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-hairline bg-panel/50 p-3">
          <span className="text-sm text-graphite">
            Move {open} unfinished {open === 1 ? "issue" : "issues"} to:
          </span>
          <button
            onClick={() => carry.mutate("")}
            className="rounded-md border border-hairline bg-paper px-3 py-1 text-xs font-semibold text-ink transition hover:border-blueprint"
          >
            Backlog
          </button>
          {others.map((o) => (
            <button
              key={o.id}
              onClick={() => carry.mutate(o.id)}
              className="rounded-md border border-hairline bg-paper px-3 py-1 text-xs font-semibold text-ink transition hover:border-blueprint"
            >
              {o.name}
            </button>
          ))}
        </div>
      )}
      {carry.isError && <p className="text-sm text-critical">{(carry.error as Error).message}</p>}
    </div>
  );
}

/**
 * Burndown plots what is actually left against the straight line from the starting
 * scope to zero.
 *
 * The guide line is deliberately not a second data series — it is a reference, so it
 * is a dashed neutral rather than a second colour competing for attention. Scope
 * changes are visible as the total line moving, which is the whole reason the
 * snapshots store the total as well as the remainder.
 */
function Burndown({ iteration }: { iteration: Iteration }) {
  const burndown = useQuery({
    queryKey: ["burndown", iteration.id],
    queryFn: () => api.iterationBurndown(iteration.id),
  });
  const [metric, setMetric] = useState<"issues" | "effort">(
    iteration.effort.estimated > 0 ? "effort" : "issues",
  );
  const points = burndown.data?.points ?? [];

  if (burndown.isLoading) {
    return <Panel title={`Burndown · ${iteration.name}`}>Loading…</Panel>;
  }
  if (points.length === 0) {
    return (
      <Panel title={`Burndown · ${iteration.name}`}>
        <p className="text-sm text-graphite-soft">
          No snapshots yet — the first one is recorded within six hours of the iteration
          becoming active. The chart is built from stored daily snapshots rather than
          recomputed, so it never rewrites its own history.
        </p>
      </Panel>
    );
  }

  const value = (p: BurndownPoint) => (metric === "issues" ? p.remaining_issues : p.remaining_minutes);
  const total = (p: BurndownPoint) => (metric === "issues" ? p.total_issues : p.total_minutes);
  const max = Math.max(...points.map((p) => Math.max(value(p), total(p))), 1);
  const fmt = (v: number) => (metric === "issues" ? String(v) : formatMinutes(v));

  // A fixed viewBox with a real aspect ratio: preserveAspectRatio="none" would stretch
  // stroke widths and turn the hover markers into ellipses.
  const W = 720;
  const H = 220;
  const PAD = { l: 44, r: 12, t: 12, b: 28 };
  const plotW = W - PAD.l - PAD.r;
  const plotH = H - PAD.t - PAD.b;

  // The x-axis spans the iteration's own window, not the days that happen to have
  // snapshots. Scaling to the data instead would put day one and day two at opposite
  // ends of the chart, and on the first day it would collapse the ideal line into a
  // vertical stroke — a shape that means nothing.
  const day = 86_400_000;
  const start = new Date(iteration.starts_on).getTime();
  const end = new Date(iteration.ends_on).getTime();
  const span = Math.max((end - start) / day, 1);
  const x = (date: string) =>
    PAD.l + Math.min(Math.max((new Date(date).getTime() - start) / day / span, 0), 1) * plotW;
  const y = (v: number) => PAD.t + plotH - (v / max) * plotH;

  const actual = points.map((p) => `${x(p.date)},${y(value(p))}`).join(" ");
  // The guide runs from the scope on day one to zero on the last day of the window.
  const guide = `${PAD.l},${y(total(points[0]))} ${PAD.l + plotW},${y(0)}`;

  return (
    <Panel
      title={`Burndown · ${iteration.name}`}
      action={
        iteration.effort.estimated > 0 && (
          <div className="flex items-center gap-1 text-xs">
            {(["effort", "issues"] as const).map((m) => (
              <button
                key={m}
                onClick={() => setMetric(m)}
                className={`rounded-md px-2 py-0.5 font-semibold capitalize transition ${
                  metric === m ? "bg-blueprint text-paper" : "text-graphite hover:text-ink"
                }`}
              >
                {m}
              </button>
            ))}
          </div>
        )
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-4 text-xs">
          <span className="flex items-center gap-1.5 text-graphite">
            <span className="h-0.5 w-4 rounded bg-blueprint" /> Remaining
          </span>
          <span className="flex items-center gap-1.5 text-graphite">
            <span className="h-0.5 w-4 rounded border-t-2 border-dashed border-graphite-soft" /> Ideal
          </span>
        </div>
        <div className="overflow-x-auto">
          <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full min-w-[420px]" role="img"
               aria-label={`Burndown for ${iteration.name}`}>
            {[0, 0.5, 1].map((t) => (
              <g key={t}>
                <line
                  x1={PAD.l} x2={W - PAD.r} y1={y(max * t)} y2={y(max * t)}
                  className="stroke-hairline" strokeWidth={1}
                />
                <text
                  x={PAD.l - 8} y={y(max * t) + 4} textAnchor="end"
                  className="fill-graphite-soft font-mono text-[10px]"
                >
                  {fmt(Math.round(max * t))}
                </text>
              </g>
            ))}
            <polyline points={guide} fill="none" strokeWidth={2} strokeDasharray="6 4"
                      className="stroke-graphite-soft" />
            <polyline points={actual} fill="none" strokeWidth={2} strokeLinejoin="round"
                      strokeLinecap="round" className="stroke-blueprint" />
            {points.map((p) => (
              <g key={p.date}>
                <circle cx={x(p.date)} cy={y(value(p))} r={4} className="fill-blueprint stroke-paper" strokeWidth={2} />
                <title>
                  {p.date}: {fmt(value(p))} remaining of {fmt(total(p))}
                </title>
              </g>
            ))}
            <text x={PAD.l} y={H - 8} className="fill-graphite-soft font-mono text-[10px]">
              {iteration.starts_on}
            </text>
            <text x={W - PAD.r} y={H - 8} textAnchor="end" className="fill-graphite-soft font-mono text-[10px]">
              {iteration.ends_on}
            </text>
          </svg>
        </div>
      </div>
    </Panel>
  );
}

/**
 * VelocityPanel reports how much finished iterations actually delivered, and how many
 * of them the average covers. An "average" over one iteration is a single fortnight
 * wearing a trend's clothes, so the sample size is stated rather than implied.
 */
function VelocityPanel({
  history,
  averageIssues,
  averageMinutes,
  averagedOver,
  windowSize,
}: {
  history: Velocity[];
  averageIssues: number;
  averageMinutes: number;
  averagedOver: number;
  windowSize: number;
}) {
  if (history.length === 0) {
    return (
      <Panel title="Velocity">
        <p className="text-sm text-graphite-soft">
          Nothing to average yet — velocity is measured from completed iterations.
        </p>
      </Panel>
    );
  }
  const fmt = sameUnit(...history.map((v) => v.done_minutes), averageMinutes);
  const max = Math.max(...history.map((v) => v.done_issues), 1);

  return (
    <Panel title="Velocity">
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="text-2xl font-bold leading-none text-ink">
            {Math.round(averageIssues * 10) / 10}
          </span>
          <span className="text-sm text-graphite">
            issues per iteration
            {averageMinutes > 0 && <> · {fmt(Math.round(averageMinutes))} of estimated work</>}
          </span>
          <span className="font-mono text-xs text-graphite-soft">
            averaged over {averagedOver} of the last {windowSize}
            {averagedOver < windowSize && " — too few to call it a trend"}
          </span>
        </div>
        <div className="flex flex-col gap-2">
          {history.slice(0, 6).map((v) => (
            <div key={v.iteration_id} className="flex items-center gap-3">
              <span className="w-24 shrink-0 truncate text-sm text-ink">{v.name}</span>
              <div className="h-2 grow overflow-hidden rounded-[4px] bg-panel">
                <div
                  className="h-full rounded-[4px] bg-blueprint"
                  style={{ width: `${Math.round((v.done_issues / max) * 100)}%` }}
                />
              </div>
              <span className="w-24 shrink-0 text-right font-mono text-xs text-graphite">
                {v.done_issues}/{v.planned_issues} done
              </span>
            </div>
          ))}
        </div>
      </div>
    </Panel>
  );
}

function Panel({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-5">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}
