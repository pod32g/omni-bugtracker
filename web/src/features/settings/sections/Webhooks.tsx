import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { timeAgo } from "../../../lib/activity";
import { Card, ErrorLine, dangerButtonClass, inputClass, quietButtonClass } from "../ui";

// Outbound webhooks admin: create, toggle, delete, and inspect deliveries.
export function WebhooksSection() {
  const qc = useQueryClient();
  const hooks = useQuery({ queryKey: ["webhooks"], queryFn: () => api.listWebhooks() });
  const [url, setUrl] = useState("");
  const [secret, setSecret] = useState("");
  const [events, setEvents] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["webhooks"] });
  const create = useMutation({
    mutationFn: () =>
      api.createWebhook({
        url: url.trim(),
        secret: secret.trim() || undefined,
        events: events.trim() ? events.split(",").map((e) => e.trim()).filter(Boolean) : undefined,
      }),
    onSuccess: () => {
      setUrl("");
      setSecret("");
      setEvents("");
      invalidate();
    },
  });
  const toggle = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) => api.updateWebhook(id, { is_active }),
    onSuccess: invalidate,
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteWebhook(id), onSuccess: invalidate });

  const items = hooks.data?.items ?? [];

  return (
    <Card
      title="Webhooks"
      description={
        <>
          POST issue events to external services. Payloads are JSON; when a secret is set, requests carry an{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">X-OBT-Signature</code> HMAC-SHA256
          header. Failed deliveries retry with backoff (8 attempts).
        </>
      }
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[2fr_1fr_1fr_auto]">
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://example.com/hook"
          className={`w-full ${inputClass}`}
        />
        <input
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder="Secret (optional)"
          className={`w-full ${inputClass}`}
        />
        <input
          value={events}
          onChange={(e) => setEvents(e.target.value)}
          placeholder="events (empty = all)"
          className={`w-full ${inputClass}`}
          title="Comma-separated, e.g. issue.created,comment.created"
        />
        <button
          disabled={!/^https?:\/\//.test(url.trim()) || create.isPending}
          onClick={() => create.mutate()}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          Add
        </button>
      </div>
      <ErrorLine error={create.error} />

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {hooks.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No webhooks yet.</div>
        )}
        {items.map((w) => (
          <div key={w.id} className="flex flex-col">
            <div className="flex items-center gap-3 p-3.5">
              <span className={`h-2 w-2 shrink-0 rounded-full ${w.is_active ? "bg-resolved" : "bg-hairline"}`} />
              <div className="min-w-0 grow">
                <div className="truncate font-mono text-sm text-ink">{w.url}</div>
                <div className="font-mono text-xs text-graphite-soft">
                  {w.events.length ? w.events.join(", ") : "all events"}
                  {w.project_key ? ` · ${w.project_key}` : " · all projects"}
                  {w.has_secret ? " · signed" : ""}
                </div>
              </div>
              <button onClick={() => setExpanded(expanded === w.id ? null : w.id)} className={quietButtonClass}>
                Deliveries
              </button>
              <button
                onClick={() => toggle.mutate({ id: w.id, is_active: !w.is_active })}
                className={quietButtonClass}
              >
                {w.is_active ? "Disable" : "Enable"}
              </button>
              <button
                onClick={() => {
                  if (window.confirm("Delete this webhook? Delivery history goes with it.")) del.mutate(w.id);
                }}
                className={dangerButtonClass}
              >
                Delete
              </button>
            </div>
            {expanded === w.id && <DeliveryLog webhookId={w.id} />}
          </div>
        ))}
      </div>
    </Card>
  );
}

function DeliveryLog({ webhookId }: { webhookId: string }) {
  const qc = useQueryClient();
  const deliveries = useQuery({
    queryKey: ["webhook-deliveries", webhookId],
    queryFn: () => api.listWebhookDeliveries(webhookId),
    refetchInterval: 5000,
  });
  const redeliver = useMutation({
    mutationFn: (deliveryId: string) => api.redeliverWebhook(webhookId, deliveryId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhook-deliveries", webhookId] }),
  });
  const items = deliveries.data?.items ?? [];
  const tone: Record<string, string> = {
    success: "text-resolved",
    failed: "text-critical",
    dead: "text-critical",
    pending: "text-graphite-soft",
  };
  return (
    <div className="flex flex-col gap-1 border-t border-hairline bg-panel/50 px-4 py-3">
      {items.length === 0 && <span className="text-xs text-graphite-soft">No deliveries yet.</span>}
      {items.map((d) => (
        <div key={d.id} className="flex items-center gap-3 font-mono text-xs">
          <span className={`w-16 font-semibold uppercase ${tone[d.status]}`}>{d.status}</span>
          <span className="w-40 truncate text-graphite">{d.event_type}</span>
          <span className="text-graphite-soft">
            {d.response_code ? `HTTP ${d.response_code}` : "—"} · try {d.attempt} · {timeAgo(d.created_at)}
          </span>
          <span className="grow" />
          {(d.status === "failed" || d.status === "dead") && (
            <button
              disabled={redeliver.isPending}
              onClick={() => redeliver.mutate(d.id)}
              className="text-blueprint hover:underline disabled:opacity-50"
            >
              Redeliver
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
