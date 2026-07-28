import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, TOKEN_SCOPES } from "../../../lib/api";
import { timeAgo } from "../../../lib/activity";
import { IconPlus } from "../../../components/icons";
import { Card, ErrorLine, fieldLabelClass, inputClass } from "../ui";

export function TokensSection() {
  const qc = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: () => api.listTokens() });
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>([]); // empty = inherit the full role
  const [created, setCreated] = useState<string | null>(null); // plaintext, shown once
  const [copied, setCopied] = useState(false);

  const create = useMutation({
    mutationFn: () => api.createToken(name.trim(), scopes),
    onSuccess: (t) => {
      setCreated(t.token);
      setName("");
      setScopes([]);
      qc.invalidateQueries({ queryKey: ["tokens"] });
    },
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeToken(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });

  const copy = () => {
    if (!created) return;
    navigator.clipboard?.writeText(created).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  const items = tokens.data?.items ?? [];

  return (
    <Card
      title="API tokens"
      description={
        <>
          Call the API with a token as{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">Authorization: Bearer obt_…</code>.
          A token authenticates as your account. Leave the scopes empty and it inherits your role in full; tick
          scopes to restrict it — scopes can only narrow what your role already allows.
        </>
      }
    >
      {created && (
        <div className="flex flex-col gap-2 rounded-md border border-resolved-border bg-resolved-soft p-4">
          <span className="font-mono text-[10px] font-medium uppercase tracking-caps text-resolved">
            New token — copy it now, you won't see it again
          </span>
          <div className="flex items-center gap-2">
            <code className="grow overflow-x-auto rounded-md border border-hairline bg-paper px-3 py-2 font-mono text-sm text-ink">
              {created}
            </code>
            <button
              onClick={copy}
              className="shrink-0 rounded-md bg-blueprint px-3 py-2 text-sm font-semibold text-paper transition hover:opacity-90"
            >
              {copied ? "Copied" : "Copy"}
            </button>
            <button onClick={() => setCreated(null)} className="shrink-0 px-2 text-sm text-graphite hover:text-ink">
              Done
            </button>
          </div>
        </div>
      )}

      <div className="flex items-end gap-3">
        <label className="grow text-sm">
          <span className={fieldLabelClass}>Token name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && name.trim()) create.mutate();
            }}
            placeholder="e.g. ci-bot, laptop, filing script"
            className={`w-full ${inputClass}`}
          />
        </label>
        <button
          disabled={!name.trim() || create.isPending}
          onClick={() => create.mutate()}
          className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          <IconPlus size={15} />
          {create.isPending ? "Creating…" : "Create token"}
        </button>
      </div>
      <div className="flex flex-col gap-2">
        <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
          Scopes {scopes.length === 0 && "· none selected (full access as you)"}
        </span>
        <div className="flex flex-wrap gap-1.5">
          {TOKEN_SCOPES.map((sc) => {
            const on = scopes.includes(sc);
            return (
              <button
                key={sc}
                onClick={() => setScopes((cur) => (on ? cur.filter((v) => v !== sc) : [...cur, sc]))}
                className={`rounded-full border px-2.5 py-1 font-mono text-[11px] transition ${
                  on
                    ? "border-blueprint bg-blueprint-soft text-blueprint"
                    : "border-hairline text-graphite-soft hover:border-graphite hover:text-graphite"
                }`}
              >
                {sc}
              </button>
            );
          })}
        </div>
      </div>
      <ErrorLine error={create.error} />

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {tokens.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {tokens.isSuccess && items.length === 0 && <div className="p-4 text-sm text-graphite-soft">No tokens yet.</div>}
        {items.map((t) => (
          <div key={t.id} className="flex items-center gap-3 p-3.5">
            <div className="grow">
              <div className="text-sm font-medium text-ink">{t.name}</div>
              <div className="font-mono text-xs text-graphite-soft">
                created {timeAgo(t.created_at)} ·{" "}
                {t.last_used_at ? `last used ${timeAgo(t.last_used_at)}` : "never used"} ·{" "}
                {t.scopes?.length ? t.scopes.join(" ") : "full access"}
              </div>
            </div>
            <button
              disabled={revoke.isPending}
              onClick={() => {
                if (window.confirm(`Revoke "${t.name}"? Any client using it stops working immediately.`))
                  revoke.mutate(t.id);
              }}
              className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical disabled:opacity-50"
            >
              Revoke
            </button>
          </div>
        ))}
      </div>
    </Card>
  );
}
