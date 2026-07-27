import type { IssueStatus, Priority, Severity, User } from "../lib/api";

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
      className="grid shrink-0 place-items-center bg-chip font-mono font-semibold text-white"
      style={{ width: size, height: size, borderRadius: radius, fontSize }}
    >
      {initials(user)}
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

// ── Labels ────────────────────────────────────────────────────────────────
export function LabelChip({ name }: { name: string }) {
  return (
    <span className="rounded-full border border-hairline bg-panel px-2 py-px text-xs font-medium text-graphite">
      {name}
    </span>
  );
}
