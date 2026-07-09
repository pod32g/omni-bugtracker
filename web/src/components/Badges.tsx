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

// ── Labels ────────────────────────────────────────────────────────────────
export function LabelChip({ name }: { name: string }) {
  return (
    <span className="rounded-full border border-hairline bg-panel px-2 py-px text-xs font-medium text-graphite">
      {name}
    </span>
  );
}
