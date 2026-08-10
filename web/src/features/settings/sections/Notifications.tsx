import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { Card, ErrorLine } from "../ui";

// How each event reads in settings, and the order they appear in — most consequential
// first, so the two people care about are at the top rather than alphabetised into the
// middle.
const EVENT_LABELS: [string, string][] = [
  ["user.mentioned", "Someone mentions me"],
  ["issue.assigned", "An issue is assigned to me"],
  ["comment.created", "Someone comments on an issue I follow"],
  ["comment.edited", "A comment I was mentioned in is edited"],
  ["issue.status_changed", "Status changes"],
  ["issue.resolved", "An issue is resolved"],
  ["issue.closed", "An issue is closed"],
  ["issue.reopened", "An issue is reopened"],
  ["issue.created", "A new issue is filed"],
  ["issue.woke", "A snoozed issue comes back"],
  ["issue.updated", "Fields or labels are edited"],
  ["issue.archived", "An issue is archived"],
  ["issue.unarchived", "An issue is unarchived"],
  ["issue.linked", "Issues are linked"],
];

const CHANNELS: [string, string][] = [
  ["off", "Off"],
  ["inbox", "Inbox"],
  ["push", "Push"],
  ["both", "Both"],
];

/**
 * NotificationsSection edits per-event routing. Saving an event back to its default
 * deletes the stored row rather than freezing a copy of it, so a later change to the
 * defaults still reaches anyone who never disagreed.
 */
export function NotificationsSection() {
  const qc = useQueryClient();
  const prefs = useQuery({ queryKey: ["notification-prefs"], queryFn: () => api.notificationPrefs() });
  const save = useMutation({
    mutationFn: (channels: Record<string, string>) => api.setNotificationPrefs(channels),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-prefs"] }),
  });

  const channels = prefs.data?.channels ?? {};
  const defaults = prefs.data?.defaults ?? {};

  return (
    <Card
      title="Notifications"
      description={
        <>
          What reaches you, and where. <span className="font-medium">Inbox</span> is in-app;{" "}
          <span className="font-medium">push</span> goes out through Omni-Notify. Muting a single issue is on the
          issue itself — you stay a watcher, it just stops talking.
        </>
      }
    >
      <ErrorLine error={prefs.error} />
      <div className="flex flex-col divide-y divide-hairline">
        {EVENT_LABELS.map(([event, label]) => (
          <div key={event} className="flex items-center gap-3 py-2">
            <span className="min-w-0 grow text-sm text-ink">{label}</span>
            {channels[event] !== defaults[event] && (
              <span className="shrink-0 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                changed
              </span>
            )}
            <select
              value={channels[event] ?? "inbox"}
              disabled={save.isPending}
              onChange={(e) => save.mutate({ ...channels, [event]: e.target.value })}
              aria-label={label}
              className="h-8 shrink-0 rounded-md border border-hairline bg-paper px-2 text-sm text-ink outline-none focus:border-blueprint disabled:opacity-60"
            >
              {CHANNELS.map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
      <ErrorLine error={save.error} />
    </Card>
  );
}
