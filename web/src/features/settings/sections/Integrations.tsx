import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type InboundIntegration } from "../../../lib/api";
import { Card, ErrorLine, inputClass, quietButtonClass } from "../ui";

const BLURB: Record<string, string> = {
  // GitHub only — git.Parse rejects every other provider value with "unsupported git
  // provider". This said "GitHub or GitLab", which sent people to configure a GitLab
  // webhook that could only ever fail.
  git: "Commit and PR webhooks from GitHub. Messages saying “fixes BUG-12” link and transition the issue.",
  logging: "Alerts from Omni-Logging. Repeated firings of one rule collapse into a single issue.",
  metrics: "Alerts from Omni-Metrics, fingerprinted the same way.",
};

/**
 * IntegrationsSection is the view of the endpoints other systems post into. They
 * authenticate by HMAC rather than a bearer token and can create issues and drive
 * transitions, so an unset secret fails closed — which, before this page existed,
 * looked from the inside exactly like nothing happening. The secret is never sent
 * back to the browser; only whether one is set, and where it came from.
 */
export function IntegrationsSection() {
  const settings = useQuery({ queryKey: ["integration-settings"], queryFn: () => api.getIntegrationSettings() });
  const inbound = settings.data?.inbound ?? [];
  const notify = settings.data?.notify;

  return (
    <>
      <Card
        title="Inbound integrations"
        description="Endpoints other systems post into. Changes here take effect on the next delivery — no redeploy — and override config.yaml until you clear them."
      >
        <ErrorLine error={settings.error} />
        {settings.isLoading && <p className="text-sm text-graphite">Loading…</p>}
        <div className="flex flex-col gap-3">
          {inbound.map((it) => (
            <InboundRow key={it.source} it={it} />
          ))}
        </div>
      </Card>

      <Card
        title="Outbound delivery"
        description="Where push notifications go. Read-only: the client is built once at startup, and which host Omni-Notify runs on is deployment topology like the database DSN — it belongs in config.yaml, not in a form."
      >
        <div className="flex flex-wrap items-center gap-3 rounded-md border border-hairline p-3.5">
          <StatusDot ok={!!notify?.enabled} />
          <div className="min-w-0 grow">
            <div className="text-sm font-medium text-ink">Omni-Notify</div>
            <div className="truncate font-mono text-xs text-graphite-soft">
              {notify?.enabled ? notify.base_url || "no base URL set" : "disabled"}
              {notify?.timeout ? ` · timeout ${notify.timeout}` : ""}
            </div>
          </div>
          <span className="shrink-0 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">config</span>
        </div>
      </Card>
    </>
  );
}

function InboundRow({ it }: { it: InboundIntegration }) {
  const qc = useQueryClient();
  const [secret, setSecret] = useState("");
  const [copied, setCopied] = useState(false);

  const save = useMutation({
    mutationFn: (patch: { enabled?: boolean; webhook_secret?: string; reset?: boolean }) =>
      api.setIntegrationSettings({ [it.source]: patch }),
    onSuccess: () => {
      setSecret("");
      qc.invalidateQueries({ queryKey: ["integration-settings"] });
    },
  });

  const copyEndpoint = () => {
    navigator.clipboard?.writeText(it.endpoint).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <div className="flex flex-col gap-3 rounded-md border border-hairline p-3.5">
      {/* Not flex-wrap: the blurbs differ in length, and wrapping put the toggle on
          its own line for some sources and not others. */}
      <div className="flex items-center gap-3">
        <StatusDot ok={it.healthy} />
        <div className="min-w-0 grow">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium capitalize text-ink">{it.source}</span>
            <span className="shrink-0 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              {it.enabled ? "on" : "off"} · {it.enabled_from}
            </span>
          </div>
          <p className="text-xs leading-relaxed text-graphite-soft">{BLURB[it.source]}</p>
        </div>
        <label className="flex shrink-0 items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={it.enabled}
            disabled={save.isPending}
            onChange={(e) => save.mutate({ enabled: e.target.checked })}
            className="h-4 w-4 accent-blueprint"
          />
          Enabled
        </label>
      </div>

      {/* The failure this whole view exists for: switched on, no secret, every
          delivery rejected with a 503 that only the sender ever sees. */}
      {!it.healthy && (
        <p className="rounded-md border border-critical/40 bg-critical-soft/40 p-2.5 text-sm text-critical">
          Enabled with no signing secret — every delivery is being rejected. Set one below, or switch it off.
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <code
          className="min-w-0 grow truncate rounded-md border border-hairline bg-panel px-2.5 py-1.5 font-mono text-xs text-graphite"
          title={it.endpoint}
        >
          {it.endpoint}
        </code>
        <button onClick={copyEndpoint} className={quietButtonClass}>
          {copied ? "Copied" : "Copy URL"}
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <input
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder={it.has_secret ? `Secret set (from ${it.secret_from}) — type to replace` : "No secret set"}
          autoComplete="new-password"
          className={`${inputClass} min-w-0 grow`}
        />
        <button
          disabled={!secret.trim() || save.isPending}
          onClick={() => save.mutate({ webhook_secret: secret.trim() })}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {save.isPending ? "Saving…" : it.has_secret ? "Rotate" : "Set secret"}
        </button>
        {(it.secret_from === "settings" || it.enabled_from === "settings") && (
          <button
            disabled={save.isPending}
            onClick={() => {
              if (window.confirm(`Revert ${it.source} to config.yaml? Any secret set here is dropped.`))
                save.mutate({ reset: true });
            }}
            className={quietButtonClass}
            title="Drop the overrides and fall back to config.yaml"
          >
            Revert to config
          </button>
        )}
      </div>
      <ErrorLine error={save.error} />
    </div>
  );
}

function StatusDot({ ok }: { ok: boolean }) {
  return <span className={`h-2 w-2 shrink-0 rounded-full ${ok ? "bg-resolved" : "bg-critical"}`} />;
}
