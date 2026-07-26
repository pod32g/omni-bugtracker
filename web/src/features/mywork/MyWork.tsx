import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Issue } from "../../lib/api";
import { timeAgo } from "../../lib/activity";
import { LabelChip, PriorityText, StatusPill } from "../../components/Badges";

/**
 * The four questions somebody actually has when they sit down, in the order they matter:
 * what is mine, who is waiting on me, what did I ask for, what am I following.
 *
 * Each section is its own query against the cross-project list, so a slow one does not
 * hold up the rest and an empty one simply disappears.
 */
const SECTIONS: { title: string; filter: string; hint: string }[] = [
  { title: "Assigned to me", filter: "is:open assignee:@me", hint: "Open issues you own" },
  { title: "Mentions you", filter: "is:open is:mentioned", hint: "Somebody named you" },
  { title: "Reported by you", filter: "is:open reporter:@me", hint: "Still open, still yours to care about" },
  { title: "Watching", filter: "is:open is:watching", hint: "Following, but not on the hook" },
];

const PER_SECTION = 8;

export function MyWork() {
  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-4 pb-5 pt-5 backdrop-blur md:px-9 md:pt-7">
        <h1 className="text-2xl font-bold leading-none tracking-[-0.02em] text-ink md:text-[30px]">My work</h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
          Across every project · not scoped to the switcher
        </p>
      </div>
      <div className="flex flex-col gap-8 px-4 py-6 md:px-9">
        {SECTIONS.map((s) => (
          <Section key={s.title} {...s} />
        ))}
      </div>
    </div>
  );
}

function Section({ title, filter, hint }: { title: string; filter: string; hint: string }) {
  // Priority order, because the point of the page is "what next", not "what is newest".
  const q = useQuery({
    queryKey: ["mywork", filter],
    queryFn: () => api.listAllIssues(filter, "priority", PER_SECTION),
    retry: false,
  });

  const items = q.data?.items ?? [];
  const total = q.data?.total ?? 0;

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-baseline gap-2.5">
        <h2 className="text-sm font-semibold text-ink">{title}</h2>
        {total > 0 && <span className="font-mono text-xs text-graphite">{total}</span>}
        <span className="hidden text-xs text-graphite-soft sm:inline">{hint}</span>
        <span className="grow" />
        {total > items.length && (
          <Link
            to={`/issues?filter=${encodeURIComponent(filter)}`}
            className="text-xs font-semibold text-blueprint transition hover:opacity-80"
          >
            View all {total}
          </Link>
        )}
      </div>

      {q.isError && <p className="text-sm text-critical">{(q.error as Error).message}</p>}
      {q.isSuccess && items.length === 0 && (
        <p className="rounded-md border border-dashed border-hairline px-3 py-4 text-xs text-graphite-soft">
          Nothing here.
        </p>
      )}
      {items.length > 0 && (
        <div className="overflow-hidden rounded-md border border-hairline">
          {items.map((issue) => (
            <Row key={issue.id} issue={issue} />
          ))}
        </div>
      )}
    </section>
  );
}

function Row({ issue }: { issue: Issue }) {
  return (
    <Link
      to={`/issues/${issue.key}`}
      className="flex items-center gap-2 border-b border-hairline bg-paper px-3 py-2.5 transition last:border-b-0 hover:bg-panel/60 sm:gap-3"
    >
      {/* The project chip is the whole point of this page — every row could be anywhere. */}
      <span className="shrink-0 rounded-sm bg-blueprint-soft px-1.5 py-0.5 font-mono text-[10px] font-semibold text-blueprint">
        {issue.project_key}
      </span>
      <span className="shrink-0 font-mono text-xs font-medium text-blueprint">{issue.key}</span>
      <span className="min-w-0 grow truncate text-sm text-ink" title={issue.title}>
        {issue.title}
      </span>
      <span className="hidden shrink-0 items-center gap-1.5 lg:flex">
        {issue.labels?.slice(0, 2).map((l) => <LabelChip key={l} name={l} />)}
      </span>
      <span className="hidden w-9 shrink-0 sm:block">
        <PriorityText priority={issue.priority} />
      </span>
      <span className="shrink-0">
        <StatusPill status={issue.status} />
      </span>
      <span className="hidden w-14 shrink-0 text-right font-mono text-xs text-graphite-soft md:block">
        {timeAgo(issue.updated_at)}
      </span>
    </Link>
  );
}
