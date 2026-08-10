// Maps internal activity verbs to human-readable phrases for the timeline and feed.
const VERBS: Record<string, string> = {
  "issue.created": "created this",
  "issue.updated": "edited this",
  "issue.deleted": "deleted this",
  "issue.status_changed": "changed the status",
  "issue.resolved": "resolved this",
  "issue.closed": "closed this",
  "issue.reopened": "reopened this",
  "comment.created": "commented",
  "comment.edited": "edited a comment",
  "issue.commit_linked": "linked a commit",
  "issue.pr_linked": "linked a pull request",
  "issue.resolved_by_git": "resolved via a commit",
  "issue.closed_by_git": "closed via a merged PR",
  "issue.auto_assigned": "was auto-assigned",
  "issue.referenced": "referenced this issue",
};

export function humanizeVerb(verb: string): string {
  return VERBS[verb] ?? verb.replace(/^issue\./, "").replace(/_/g, " ");
}

/**
 * describeActivity is humanizeVerb plus whatever the entry's payload adds. Auto-assignment
 * is meaningless without the reason — "was auto-assigned" leaves the assignee guessing who
 * decided that, which is exactly the complaint routing is supposed to avoid.
 */
export function describeActivity(a: { verb: string; changes?: Record<string, unknown> | null }): string {
  const base = humanizeVerb(a.verb);
  const c = a.changes;
  if (!c) return base;
  if (a.verb === "issue.auto_assigned" && c.reason === "component_lead" && typeof c.component === "string") {
    return `${base} as lead of ${c.component}`;
  }
  if (a.verb === "issue.referenced" && typeof c.from === "string") {
    return `referenced this from ${c.from}`;
  }
  return base;
}

export function timeAgo(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  const m = s / 60;
  if (m < 60) return `${Math.floor(m)}m ago`;
  const h = m / 60;
  if (h < 24) return `${Math.floor(h)}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
