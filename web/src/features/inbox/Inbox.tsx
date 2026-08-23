import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Notification } from "../../lib/api";
import { timeAgo } from "../../lib/activity";
import { Avatar, StatusPill } from "../../components/Badges";

/** How each event reads in the inbox. Anything unmapped falls back to the raw verb. */
const EVENT_TEXT: Record<string, string> = {
  "user.mentioned": "mentioned you",
  "comment.created": "commented",
  "comment.edited": "edited a comment",
  "issue.assigned": "assigned this to you",
  "issue.created": "filed this",
  "issue.status_changed": "changed the status",
  "issue.resolved": "resolved this",
  "issue.closed": "closed this",
  "issue.reopened": "reopened this",
  "issue.woke": "came back from snooze",
  "issue.archived": "archived this",
};

function describe(n: Notification): string {
  return EVENT_TEXT[n.event_type] ?? n.event_type.replace(/^issue\./, "").replace(/_/g, " ");
}

export function Inbox() {
  const qc = useQueryClient();
  const inbox = useQuery({ queryKey: ["notifications", false], queryFn: () => api.notifications(false, 50) });
  const markRead = useMutation({
    mutationFn: (ids: string[]) => api.markNotificationsRead(ids),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }),
  });

  const items = inbox.data?.items ?? [];
  const unread = inbox.data?.unread ?? 0;

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-wrap items-end justify-between gap-3 border-b border-hairline bg-paper/80 px-4 pb-5 pt-5 backdrop-blur md:px-9 md:pt-7">
        <div className="flex flex-col gap-1.5">
          <h1 className="text-2xl font-bold leading-none tracking-[-0.02em] text-ink md:text-[30px]">Inbox</h1>
          <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
            {unread} unread · {items.length} recent
          </p>
        </div>
        {unread > 0 && (
          <button
            onClick={() => markRead.mutate([])}
            disabled={markRead.isPending}
            className="h-9 rounded-md border border-hairline px-3 text-sm font-medium text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50"
          >
            {markRead.isPending ? "Marking…" : "Mark all read"}
          </button>
        )}
      </div>

      {inbox.isLoading && <p className="px-4 py-8 text-sm text-graphite md:px-9">Loading…</p>}
      {inbox.isError && (
        <p className="px-4 py-8 text-sm text-critical md:px-9">{(inbox.error as Error).message}</p>
      )}
      {inbox.isSuccess && items.length === 0 && (
        <p className="px-4 py-10 text-sm text-graphite-soft md:px-9">
          Nothing yet. You'll hear about issues you watch, and anything that mentions you.
        </p>
      )}

      {items.map((n) => (
        <Link
          key={n.id}
          to={`/issues/${n.issue_key}`}
          className={`flex items-center gap-3 border-b border-hairline px-4 py-3 transition hover:bg-panel/60 md:px-9 ${
            n.read_at ? "" : "bg-blueprint-soft/25"
          }`}
        >
          {/* An unread marker in the gutter rather than bold text: the row stays legible
              either way, and the eye scans one column instead of two weights. */}
          <span
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${n.read_at ? "bg-transparent" : "bg-blueprint"}`}
            aria-label={n.read_at ? "read" : "unread"}
          />
          <Avatar user={n.actor} size={24} />
          <span className="shrink-0 font-mono text-xs font-medium text-blueprint">{n.issue_key}</span>
          <span className="min-w-0 grow truncate text-sm text-ink" title={n.issue_title}>
            <span className="font-medium">{n.actor?.display_name ?? "system"}</span>{" "}
            <span className="text-graphite">{describe(n)}</span> — {n.issue_title}
          </span>
          <span className="hidden shrink-0 sm:block">
            <StatusPill status={n.issue_status} />
          </span>
          <span className="shrink-0 font-mono text-xs text-graphite-soft">{timeAgo(n.created_at)}</span>
        </Link>
      ))}
    </div>
  );
}
