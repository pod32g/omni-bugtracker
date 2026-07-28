import { useQuery } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { Card, ErrorLine } from "../ui";

/**
 * OperationsSection is the operator view. The queue does the real work — event fan-out,
 * notify, webhooks, indexing, automation, git ingest — and none of it was visible from
 * inside the app: when notify was failing against an unreachable host, nothing in the
 * product said so. Refreshes on an interval because a stuck queue is a thing you watch,
 * not a thing you reload for.
 */
export function OperationsSection() {
  const ops = useQuery({ queryKey: ["ops"], queryFn: () => api.ops(), refetchInterval: 15_000, retry: false });
  const d = ops.data;

  // Anything not completed is work outstanding; retryable and discarded are the two
  // that mean something is wrong rather than merely pending.
  const stuck = (d?.queues ?? []).filter((q) => q.state === "retryable" || q.state === "discarded");

  return (
    <Card
      title="Operations"
      description={
        <>
          Background queue and outbound delivery health. Refreshes every 15s.
          {d?.process && (
            <span className="ml-1 font-mono text-xs text-graphite-soft">
              up {Math.floor(d.process.uptime_seconds / 60)}m · {d.process.go_version} · {d.process.goroutines}{" "}
              goroutines
            </span>
          )}
        </>
      }
    >
      <ErrorLine error={ops.error} />
      {d?.queue_error && (
        <p className="rounded-md border border-critical/40 bg-critical-soft/40 p-3 text-sm text-critical">
          Queue tables unreadable: {d.queue_error}
        </p>
      )}

      {stuck.length > 0 && (
        <p className="rounded-md border border-critical/40 bg-critical-soft/40 p-3 text-sm text-critical">
          {stuck.map((q) => `${q.count} ${q.state} on ${q.queue}`).join(" · ")}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        {(d?.queues ?? []).map((q) => (
          <span
            key={`${q.queue}-${q.state}`}
            className={`rounded-md border px-2.5 py-1 font-mono text-xs ${
              q.state === "retryable" || q.state === "discarded"
                ? "border-critical/40 bg-critical-soft/40 text-critical"
                : "border-hairline text-graphite"
            }`}
          >
            {q.queue} · {q.state} {q.count}
          </span>
        ))}
        {d?.queues.length === 0 && <span className="text-sm text-graphite-soft">Queue is empty.</span>}
      </div>

      {(d?.failures ?? []).length > 0 && (
        <div className="flex flex-col gap-1.5">
          <p className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Recent failures</p>
          {d!.failures.map((f, i) => (
            <div key={i} className="flex items-baseline gap-2 text-xs">
              <span className="w-32 shrink-0 truncate font-mono text-graphite">{f.kind}</span>
              <span className="shrink-0 font-mono text-graphite-soft">try {f.attempt}</span>
              <span className="min-w-0 grow truncate text-critical" title={f.error}>
                {f.error}
              </span>
            </div>
          ))}
        </div>
      )}

      {(d?.deliveries ?? []).length > 0 && (
        <div className="flex flex-col gap-1.5">
          <p className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
            Webhook deliveries · last 24h
          </p>
          {d!.deliveries.map((w) => (
            <div key={w.url} className="flex items-baseline gap-2 text-xs">
              <span className="min-w-0 grow truncate font-mono text-graphite" title={w.url}>
                {w.url}
              </span>
              <span className={w.succeeded === w.total ? "text-resolved" : "text-critical"}>
                {w.succeeded}/{w.total}
              </span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
