import type { IssueStatus, Priority, Severity, User } from "../lib/api";

const severityColor: Record<Severity, string> = {
  critical: "bg-severity-critical/20 text-severity-critical",
  high: "bg-severity-high/20 text-severity-high",
  medium: "bg-severity-medium/20 text-severity-medium",
  low: "bg-severity-low/20 text-severity-low",
};

const statusLabel: Record<IssueStatus, string> = {
  open: "Open",
  in_progress: "In Progress",
  blocked: "Blocked",
  ready_for_review: "Ready for Review",
  resolved: "Resolved",
  closed: "Closed",
  reopened: "Reopened",
};

export function SeverityBadge({ severity }: { severity?: Severity }) {
  if (!severity) return null;
  return (
    <span className={`rounded px-2 py-0.5 text-xs font-medium ${severityColor[severity]}`}>
      {severity}
    </span>
  );
}

export function StatusBadge({ status }: { status: IssueStatus }) {
  return (
    <span className="rounded border border-surface-border bg-white/5 px-2 py-0.5 text-xs text-slate-300">
      {statusLabel[status]}
    </span>
  );
}

export function Avatar({ user }: { user?: User }) {
  if (!user) {
    return (
      <span
        title="Unassigned"
        className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-white/5 text-[10px] text-slate-500"
      >
        –
      </span>
    );
  }
  const initials = (user.display_name || user.email || "?").slice(0, 2).toUpperCase();
  return (
    <span
      title={user.display_name || user.email}
      className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-accent/25 text-[10px] font-medium text-accent-hover"
    >
      {initials}
    </span>
  );
}

export function LabelChip({ name }: { name: string }) {
  return (
    <span className="rounded-full bg-white/5 px-2 py-0.5 text-xs text-slate-300">{name}</span>
  );
}

export function PriorityBadge({ priority }: { priority: Priority }) {
  return (
    <span className="rounded bg-white/5 px-1.5 py-0.5 font-mono text-xs text-slate-400">
      {priority.toUpperCase()}
    </span>
  );
}
