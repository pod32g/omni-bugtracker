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
  "issue.commit_linked": "linked a commit",
  "issue.pr_linked": "linked a pull request",
  "issue.resolved_by_git": "resolved via a commit",
  "issue.closed_by_git": "closed via a merged PR",
};

export function humanizeVerb(verb: string): string {
  return VERBS[verb] ?? verb.replace(/^issue\./, "").replace(/_/g, " ");
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
