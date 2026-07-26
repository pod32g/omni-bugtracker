import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Report } from "../../lib/api";
import { useProject } from "../../lib/project";

/**
 * Reports: the trend view the dashboard cannot give, because the dashboard is entirely
 * point-in-time and nothing there shows a direction.
 *
 * Two series (created, resolved) use the app's own blueprint/resolved tokens. That pair
 * was run through the palette validator: in light mode every check passes (CVD ΔE 26.6
 * deutan, normal-vision 29.9, contrast ≥3:1); in dark mode separation and contrast pass
 * and only the lightness-band check flags, which is about visual weight rather than
 * legibility. Identity is never carried by colour alone regardless — there is a legend
 * and both lines are directly labelled at their right-hand end.
 */

const RANGES: { label: string; days: number }[] = [
  { label: "30 days", days: 30 },
  { label: "90 days", days: 90 },
  { label: "1 year", days: 365 },
];

// Ordered worst-first, which is the order somebody reads a backlog in.
const SEVERITIES = ["critical", "high", "medium", "low", "none"];
const AGE_BUCKETS = ["0-7d", "7-30d", "30-90d", "90d+"];

export function Reports() {
  const { projectKey } = useProject();
  const [days, setDays] = useState(90);
  const report = useQuery({
    queryKey: ["report", projectKey, days],
    queryFn: () => api.report(projectKey, days),
    enabled: !!projectKey,
  });

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-wrap items-end justify-between gap-3 border-b border-hairline bg-paper/80 px-4 pb-5 pt-5 backdrop-blur md:px-9 md:pt-7">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-2xl font-bold leading-none tracking-[-0.02em] text-ink md:text-[30px]">Reports</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {projectKey || "no project"} · last {days} days
          </p>
        </div>
        {/* Filters in one row above the charts. */}
        <div className="flex gap-1.5">
          {RANGES.map((r) => (
            <button
              key={r.days}
              onClick={() => setDays(r.days)}
              className={`h-8 rounded-full px-3 text-sm transition ${
                days === r.days
                  ? "bg-blueprint font-semibold text-paper"
                  : "border border-hairline text-graphite hover:border-graphite hover:text-ink"
              }`}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {report.isLoading && <p className="px-4 py-8 text-sm text-graphite md:px-9">Loading…</p>}
      {report.isError && (
        <p className="px-4 py-8 text-sm text-critical md:px-9">{(report.error as Error).message}</p>
      )}

      {report.data && (
        <div className="flex flex-col gap-8 px-4 py-6 md:px-9">
          <Percentiles r={report.data} />
          <FlowChart flow={report.data.flow} />
          <AgeChart age={report.data.age} />
          <ThroughputChart items={report.data.throughput} />
        </div>
      )}
    </div>
  );
}

/** A duration is a headline, not a chart — four numbers with their sample size. */
function Percentiles({ r }: { r: Report }) {
  const tiles = [
    { label: "Time to resolve · median", hours: r.resolve_p50_hours, n: r.resolved_count },
    { label: "Time to resolve · p90", hours: r.resolve_p90_hours, n: r.resolved_count },
    { label: "First response · median", hours: r.respond_p50_hours, n: r.responded_count },
    { label: "First response · p90", hours: r.respond_p90_hours, n: r.responded_count },
  ];
  return (
    <section className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {tiles.map((t) => (
        <div key={t.label} className="rounded-lg border border-hairline bg-paper p-4">
          <p className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">{t.label}</p>
          <p className="mt-1.5 text-2xl font-bold text-ink">{t.n === 0 ? "—" : formatHours(t.hours)}</p>
          <p className="mt-0.5 text-xs text-graphite-soft">
            {t.n === 0 ? "nothing in range" : `over ${t.n} ${t.n === 1 ? "issue" : "issues"}`}
          </p>
        </div>
      ))}
    </section>
  );
}

/**
 * Created vs resolved per week — the one chart that shows whether the backlog is
 * growing. Two lines on one axis; never two scales.
 */
function FlowChart({ flow }: { flow: Report["flow"] }) {
  const [hover, setHover] = useState<number | null>(null);
  if (flow.length === 0) return null;

  // 6:1 rather than a fixed pixel height: scaling the viewBox non-uniformly would
  // stretch stroke widths and turn the hover markers into ellipses. A wider box gets
  // the same ~200px on a desktop pane with the geometry intact.
  const w = 1200;
  const h = 200;
  const pad = { l: 32, r: 56, t: 12, b: 24 };
  const max = Math.max(4, ...flow.map((p) => Math.max(p.created, p.resolved)));
  const x = (i: number) => pad.l + (i * (w - pad.l - pad.r)) / Math.max(1, flow.length - 1);
  const y = (v: number) => pad.t + (1 - v / max) * (h - pad.t - pad.b);
  const path = (key: "created" | "resolved") =>
    flow.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(" ");

  const last = flow[flow.length - 1];

  return (
    <ChartFrame
      title="Created vs resolved"
      hint="Per week. Lines converging means the backlog is holding; created above resolved means it is growing."
      legend={[
        { label: "Created", className: "bg-blueprint" },
        { label: "Resolved", className: "bg-resolved" },
      ]}
    >
      <svg viewBox={`0 0 ${w} ${h}`} className="w-full" role="img" aria-label="Issues created and resolved per week">
        {/* Recessive grid: three references, no box. */}
        {[0, 0.5, 1].map((f) => (
          <line
            key={f}
            x1={pad.l}
            x2={w - pad.r}
            y1={y(max * f)}
            y2={y(max * f)}
            className="stroke-hairline"
            strokeWidth={1}
          />
        ))}
        <text x={4} y={y(max) + 4} className="fill-graphite-soft text-[10px]">{max}</text>
        <text x={4} y={y(0) + 4} className="fill-graphite-soft text-[10px]">0</text>

        <path d={path("created")} fill="none" className="stroke-blueprint" strokeWidth={2} strokeLinejoin="round" />
        <path d={path("resolved")} fill="none" className="stroke-resolved" strokeWidth={2} strokeLinejoin="round" />

        {/* Direct labels at the right-hand end, so identity survives without the legend. */}
        <text x={w - pad.r + 6} y={y(last.created) + 3} className="fill-blueprint text-[10px] font-medium">
          {last.created} created
        </text>
        <text x={w - pad.r + 6} y={y(last.resolved) + 3} className="fill-resolved text-[10px] font-medium">
          {last.resolved} resolved
        </text>

        {/* Hover: a crosshair per week, with hit targets far wider than the marks. */}
        {flow.map((p, i) => (
          <g key={p.period} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
            <rect
              x={x(i) - (w - pad.l - pad.r) / (2 * Math.max(1, flow.length - 1))}
              y={pad.t}
              width={(w - pad.l - pad.r) / Math.max(1, flow.length - 1)}
              height={h - pad.t - pad.b}
              fill="transparent"
            />
            {hover === i && (
              <>
                <line x1={x(i)} x2={x(i)} y1={pad.t} y2={h - pad.b} className="stroke-graphite-soft" strokeWidth={1} />
                {/* A 2px surface ring keeps the marker readable over the line. */}
                <circle cx={x(i)} cy={y(p.created)} r={4} className="fill-blueprint stroke-paper" strokeWidth={2} />
                <circle cx={x(i)} cy={y(p.resolved)} r={4} className="fill-resolved stroke-paper" strokeWidth={2} />
              </>
            )}
          </g>
        ))}
      </svg>
      <p className="h-4 font-mono text-[11px] text-graphite">
        {hover !== null
          ? `week of ${flow[hover].period.slice(0, 10)} — ${flow[hover].created} created, ${flow[hover].resolved} resolved`
          : ""}
      </p>
    </ChartFrame>
  );
}

/**
 * Backlog age, faceted by severity rather than stacked.
 *
 * A stacked bar would need four adjacent severity colours to carry the distinction, and
 * the validator rejects that palette: critical↔high sit at ΔE 13.2 for normal vision,
 * below the floor of 15, and the low/none greys fall under the chroma floor. So severity
 * is carried by the row label and colour is not load-bearing at all.
 */
function AgeChart({ age }: { age: Report["age"] }) {
  const bySeverity = new Map<string, Map<string, number>>();
  for (const a of age) {
    if (!bySeverity.has(a.severity)) bySeverity.set(a.severity, new Map());
    bySeverity.get(a.severity)!.set(a.bucket, a.count);
  }
  const rows = SEVERITIES.filter((s) => bySeverity.has(s));
  if (rows.length === 0) return null;
  const max = Math.max(1, ...age.map((a) => a.count));

  return (
    <ChartFrame
      title="Open backlog by age"
      hint="How old the unfinished work is. Anything heavy on the right has been waiting a long time."
    >
      <div className="flex flex-col gap-3">
        <div className="flex gap-2 pl-20 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
          {AGE_BUCKETS.map((b) => (
            <span key={b} className="flex-1">{b}</span>
          ))}
        </div>
        {rows.map((sev) => (
          <div key={sev} className="flex items-center gap-2">
            <span className="w-[72px] shrink-0 text-right text-xs font-medium capitalize text-ink">{sev}</span>
            {AGE_BUCKETS.map((bucket) => {
              const count = bySeverity.get(sev)?.get(bucket) ?? 0;
              return (
                <Link
                  key={bucket}
                  to={`/issues?filter=${encodeURIComponent(`is:open severity:${sev}`)}`}
                  title={`${count} ${sev} open ${bucket}`}
                  aria-label={`${count} ${sev} issues open ${bucket}`}
                  className="flex h-7 flex-1 items-center rounded bg-panel transition hover:opacity-80"
                >
                  {/* 4px rounded data-end anchored to the baseline; the count is a direct
                      label rather than a tooltip-only value. */}
                  <span
                    className="h-full rounded bg-blueprint/70"
                    style={{ width: `${Math.max(count === 0 ? 0 : 6, (count / max) * 100)}%` }}
                  />
                  <span className="pl-1.5 font-mono text-[11px] text-graphite">{count || ""}</span>
                </Link>
              );
            })}
          </div>
        ))}
      </div>
    </ChartFrame>
  );
}

/** One series, so no legend — the title names it. Direct labels on every bar. */
function ThroughputChart({ items }: { items: Report["throughput"] }) {
  if (items.length === 0) return null;
  const max = Math.max(...items.map((i) => i.count));
  return (
    <ChartFrame title="Resolved per person" hint="Who closed what over the range.">
      <div className="flex flex-col gap-1.5">
        {items.map((t) => (
          <div key={t.name} className="flex items-center gap-2">
            <span className="w-32 shrink-0 truncate text-right text-xs text-ink" title={t.name}>
              {t.name}
            </span>
            <span className="flex h-6 grow items-center">
              <span
                className="h-full rounded bg-resolved/70"
                style={{ width: `${(t.count / max) * 100}%` }}
              />
              <span className="pl-2 font-mono text-[11px] text-graphite">{t.count}</span>
            </span>
          </div>
        ))}
      </div>
    </ChartFrame>
  );
}

function ChartFrame({
  title,
  hint,
  legend,
  children,
}: {
  title: string;
  hint: string;
  legend?: { label: string; className: string }[];
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3 rounded-lg border border-hairline bg-paper p-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold text-ink">{title}</h2>
          <p className="mt-0.5 text-xs text-graphite-soft">{hint}</p>
        </div>
        {legend && (
          <div className="flex items-center gap-3">
            {legend.map((l) => (
              <span key={l.label} className="flex items-center gap-1.5 text-xs text-graphite">
                <span className={`h-2 w-2 rounded-full ${l.className}`} />
                {l.label}
              </span>
            ))}
          </div>
        )}
      </div>
      {children}
    </section>
  );
}

function formatHours(h: number): string {
  if (h < 1) return `${Math.round(h * 60)}m`;
  if (h < 48) return `${h.toFixed(1)}h`;
  return `${(h / 24).toFixed(1)}d`;
}
