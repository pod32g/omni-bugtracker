import type { IssueStatus, Priority, Severity, User } from "../lib/api";
import { formatMinutes, formatMinutesLong, formatPair, sameUnit } from "../lib/duration";

// ── Status ────────────────────────────────────────────────────────────────
export const statusLabel: Record<IssueStatus, string> = {
  open: "Open",
  in_progress: "In Progress",
  blocked: "Blocked",
  ready_for_review: "In Review",
  resolved: "Resolved",
  closed: "Closed",
  reopened: "Reopened",
};

type Tone = { text: string; bg: string; border: string; dot: string };

// Semantic color per lifecycle state, derived from the design tokens.
export const statusTone: Record<IssueStatus, Tone> = {
  open: { text: "text-blueprint", bg: "bg-blueprint-soft", border: "border-blueprint-border", dot: "bg-blueprint" },
  reopened: { text: "text-blueprint", bg: "bg-blueprint-soft", border: "border-blueprint-border", dot: "bg-blueprint" },
  ready_for_review: { text: "text-blueprint", bg: "bg-blueprint-soft", border: "border-blueprint-border", dot: "bg-blueprint" },
  in_progress: { text: "text-high", bg: "bg-high-soft", border: "border-high-border", dot: "bg-high" },
  blocked: { text: "text-critical", bg: "bg-critical-soft", border: "border-critical-border", dot: "bg-critical" },
  resolved: { text: "text-resolved", bg: "bg-resolved-soft", border: "border-resolved-border", dot: "bg-resolved" },
  closed: { text: "text-graphite", bg: "bg-panel", border: "border-hairline", dot: "bg-graphite" },
};

export function StatusPill({ status }: { status: IssueStatus }) {
  const t = statusTone[status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold ${t.bg} ${t.border} ${t.text}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${t.dot}`} />
      {statusLabel[status]}
    </span>
  );
}

/**
 * StatusDot is StatusPill's compact form: the same colour, no label, the name in a
 * tooltip. For dense lists where the title has to win the fight for horizontal space.
 */
export function StatusDot({ status }: { status: IssueStatus }) {
  return (
    <span
      title={statusLabel[status]}
      aria-label={statusLabel[status]}
      className={`h-2 w-2 shrink-0 rounded-full ${statusTone[status].dot}`}
    />
  );
}

// ── Severity ──────────────────────────────────────────────────────────────
// The colored marker (dot + left rail bar) reads severity; the label stays ink.
const severityColor: Record<Severity, string> = {
  critical: "bg-critical",
  high: "bg-high",
  medium: "bg-medium",
  low: "bg-graphite-soft",
};

export function SeverityBar({ severity }: { severity?: Severity }) {
  return <span className={`h-6 w-[3px] shrink-0 rounded-[2px] ${severity ? severityColor[severity] : "bg-graphite-soft"}`} />;
}

export function SeverityMark({ severity }: { severity?: Severity }) {
  if (!severity) return <span className="text-sm text-graphite-soft">—</span>;
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`h-[7px] w-[7px] rounded-full ${severityColor[severity]}`} />
      <span className="text-sm font-medium capitalize text-ink">{severity}</span>
    </span>
  );
}

const severityPillTone: Record<Severity, Tone> = {
  critical: { text: "text-critical", bg: "bg-critical-soft", border: "border-critical-border", dot: "bg-critical" },
  high: { text: "text-high", bg: "bg-high-soft", border: "border-high-border", dot: "bg-high" },
  medium: { text: "text-medium", bg: "bg-panel", border: "border-hairline", dot: "bg-medium" },
  low: { text: "text-graphite", bg: "bg-panel", border: "border-hairline", dot: "bg-graphite-soft" },
};

export function SeverityPill({ severity }: { severity?: Severity }) {
  if (!severity) return null;
  const t = severityPillTone[severity];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold capitalize ${t.bg} ${t.border} ${t.text}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${t.dot}`} />
      {severity}
    </span>
  );
}

// ── Priority ──────────────────────────────────────────────────────────────
// Emphasis scales down with priority: p0/p1 solid ink, then muted.
const priorityColor: Record<Priority, string> = {
  p0: "font-semibold text-ink",
  p1: "font-semibold text-ink",
  p2: "font-medium text-graphite",
  p3: "font-medium text-graphite-soft",
};

export function PriorityText({ priority }: { priority: Priority }) {
  return <span className={`font-mono text-sm ${priorityColor[priority]}`}>{priority.toUpperCase()}</span>;
}

export function PriorityChip({ priority }: { priority: Priority }) {
  return (
    <span className="rounded-sm border border-hairline bg-panel px-2 py-0.5 font-mono text-xs font-semibold text-ink">
      {priority.toUpperCase()}
    </span>
  );
}

// ── Avatar ────────────────────────────────────────────────────────────────
export function initials(user?: User): string {
  return (user?.display_name || user?.email || "?").slice(0, 2).toUpperCase();
}

export function Avatar({ user, size = 28 }: { user?: User; size?: number }) {
  const fontSize = Math.round(size * 0.42);
  const radius = Math.max(5, Math.round(size * 0.25));
  if (!user) {
    return (
      <span
        title="Unassigned"
        className="grid shrink-0 place-items-center bg-chip-empty font-mono font-semibold text-graphite"
        style={{ width: size, height: size, borderRadius: radius, fontSize }}
      >
        –
      </span>
    );
  }
  return (
    <span
      title={user.display_name || user.email}
      className="grid shrink-0 place-items-center overflow-hidden bg-chip font-mono font-semibold text-white"
      style={{ width: size, height: size, borderRadius: radius, fontSize }}
    >
      {/* The image sits over the initials rather than replacing them, so a URL that
          404s or a host that blocks hotlinking degrades to the initials underneath
          instead of leaving a blank square. */}
      {user.avatar_url ? (
        <>
          <span className="col-start-1 row-start-1">{initials(user)}</span>
          <img
            src={user.avatar_url}
            alt=""
            loading="lazy"
            className="col-start-1 row-start-1 h-full w-full object-cover"
            onError={(e) => {
              e.currentTarget.style.display = "none";
            }}
          />
        </>
      ) : (
        initials(user)
      )}
    </span>
  );
}

// ── Due dates & SLA ───────────────────────────────────────────────────────

/** relativeDue renders a deadline the way people say it out loud. */
export function relativeDue(iso: string, now = Date.now()): string {
  const ms = new Date(iso).getTime() - now;
  const days = Math.round(ms / 86_400_000);
  const hours = Math.round(ms / 3_600_000);
  if (ms < 0) {
    const late = Math.abs(days);
    if (late === 0) return `${Math.abs(hours)}h late`;
    return late === 1 ? "1 day late" : `${late} days late`;
  }
  if (hours < 24) return `in ${hours}h`;
  return days === 1 ? "tomorrow" : `in ${days} days`;
}

/**
 * DueChip shows a deadline, emphasised only when it is actually a problem. A
 * resolved issue never reads as late — the work is done, and colouring history red
 * would make the whole column noise.
 */
export function DueChip({
  dueAt,
  resolved,
  now,
}: {
  dueAt?: string | null;
  resolved?: boolean;
  now?: number;
}) {
  if (!dueAt) return null;
  const overdue = !resolved && new Date(dueAt).getTime() < (now ?? Date.now());
  return (
    <span
      title={new Date(dueAt).toLocaleString()}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-px text-xs font-medium ${
        overdue
          ? "border-critical-border bg-critical-soft text-critical"
          : "border-hairline bg-panel text-graphite"
      }`}
    >
      {overdue ? "⚠" : "⏱"} {relativeDue(dueAt, now)}
    </span>
  );
}

/**
 * SLAPill renders the worse of an issue's two SLA states. "ok" and "met" render
 * nothing: a pill on every issue that is simply fine is a pill nobody reads, and the
 * two states worth interrupting someone over lose their weight next to it.
 */
export function SLAPill({ sla }: { sla?: { state: string } }) {
  if (!sla || (sla.state !== "breached" && sla.state !== "at_risk")) return null;
  const breached = sla.state === "breached";
  return (
    <span
      title={breached ? "Past its SLA target" : "Approaching its SLA target"}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-px text-xs font-semibold ${
        breached
          ? "border-critical-border bg-critical-soft text-critical"
          : "border-high-border bg-high-soft text-high"
      }`}
    >
      SLA {breached ? "breached" : "at risk"}
    </span>
  );
}

// ── Effort ────────────────────────────────────────────────────────────────

/**
 * EffortBar shows estimate vs spent as one bar rather than two numbers.
 *
 * The bar is the estimate; the fill is what has been spent against it. Over-run
 * turns the fill critical and clamps at full width instead of overflowing — a bar
 * that runs off its own track is a rendering bug, not a signal.
 */
export function EffortBar({
  estimateMinutes,
  spentMinutes,
  className = "",
}: {
  estimateMinutes?: number | null;
  spentMinutes?: number;
  className?: string;
}) {
  const estimate = estimateMinutes ?? 0;
  const spent = spentMinutes ?? 0;
  if (!estimate && !spent) return null;

  // Unestimated but with time logged: there is no denominator, so show the number
  // rather than a bar that would imply a budget nobody set.
  if (!estimate) {
    return (
      <span className={`text-xs font-medium text-graphite ${className}`}>
        {formatMinutes(spent)} spent · no estimate
      </span>
    );
  }
  const ratio = Math.min(spent / estimate, 1);
  const over = spent > estimate;
  return (
    <span className={`flex flex-col gap-1 ${className}`}>
      <span className="flex items-baseline justify-between gap-2 text-xs">
        <span
          className={`font-medium ${over ? "text-critical" : "text-graphite"}`}
          title={`${formatMinutesLong(spent)} spent against ${formatMinutesLong(estimate)} estimated`}
        >
          {formatPair(spent, estimate)}
        </span>
        {over && <span className="font-semibold text-critical">over</span>}
      </span>
      <span className="h-1.5 w-full overflow-hidden rounded-full bg-panel">
        <span
          className={`block h-full rounded-full ${over ? "bg-critical" : "bg-blueprint"}`}
          style={{ width: `${ratio * 100}%` }}
        />
      </span>
    </span>
  );
}

/**
 * EffortNote is the one-line rollup for a milestone/release/component row.
 *
 * It leads with coverage when the set is only partly estimated, because "3w
 * estimated" over eight of forty issues describes a fifth of the work and reads
 * like all of it.
 */
export function EffortNote({ effort }: { effort?: { issues: number; estimated: number; estimate_minutes: number; spent_minutes: number } }) {
  if (!effort || (effort.estimated === 0 && effort.spent_minutes === 0)) return null;
  const partial = effort.estimated > 0 && effort.estimated < effort.issues;
  const fmt = sameUnit(effort.estimate_minutes, effort.spent_minutes);
  return (
    <span title={`${effort.estimated} of ${effort.issues} issues estimated`}>
      {effort.estimated > 0 && <>{fmt(effort.estimate_minutes)} est</>}
      {partial && <span className="text-graphite-soft"> ({effort.estimated}/{effort.issues})</span>}
      {effort.spent_minutes > 0 && (
        <>
          {effort.estimated > 0 && " · "}
          {fmt(effort.spent_minutes)} spent
        </>
      )}
    </span>
  );
}

/**
 * ChecklistChip shows `- [ ]` progress from the body — a definition-of-done nobody can
 * see the state of without opening the issue is one nobody uses. Goes solid when
 * everything is ticked, which is the only state worth a colour.
 */
export function ChecklistChip({ progress }: { progress?: { done: number; total: number } }) {
  if (!progress || progress.total === 0) return null;
  const complete = progress.done === progress.total;
  return (
    <span
      title={`${progress.done} of ${progress.total} checklist items done`}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-px font-mono text-xs ${
        complete
          ? "border-resolved-border bg-resolved-soft text-resolved"
          : "border-hairline bg-panel text-graphite"
      }`}
    >
      {complete ? "☑" : "☐"} {progress.done}/{progress.total}
    </span>
  );
}

/** EstimateChip is the compact list-row form: just the estimate, muted. */
export function EstimateChip({ minutes }: { minutes?: number | null }) {
  if (!minutes) return null;
  return (
    <span
      title={`Estimated ${formatMinutesLong(minutes)}`}
      className="rounded-full border border-hairline bg-panel px-2 py-px font-mono text-xs text-graphite"
    >
      {formatMinutes(minutes)}
    </span>
  );
}

// ── Labels ────────────────────────────────────────────────────────────────
export function LabelChip({ name }: { name: string }) {
  return (
    <span className="rounded-full border border-hairline bg-panel px-2 py-px text-xs font-medium text-graphite">
      {name}
    </span>
  );
}
