import type { IssueStatus, Priority, Severity } from "../lib/api";

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

export function PriorityBadge({ priority }: { priority: Priority }) {
  return (
    <span className="rounded bg-white/5 px-1.5 py-0.5 font-mono text-xs text-slate-400">
      {priority.toUpperCase()}
    </span>
  );
}
