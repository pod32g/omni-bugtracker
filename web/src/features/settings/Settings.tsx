import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type User } from "../../lib/api";
import { timeAgo } from "../../lib/activity";
import { Avatar } from "../../components/Badges";
import { IconPlus } from "../../components/icons";

const ROLES = ["owner", "admin", "maintainer", "member", "reporter", "bot"];

export function Settings() {
  const qc = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: () => api.listTokens() });
  const [name, setName] = useState("");
  const [created, setCreated] = useState<string | null>(null); // plaintext, shown once
  const [copied, setCopied] = useState(false);

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManageRoles = ["owner", "admin"].includes(me.data?.role ?? "");
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers(), enabled: canManageRoles });
  const setUserRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => api.updateUserRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const create = useMutation({
    mutationFn: () => api.createToken(name.trim()),
    onSuccess: (t) => {
      setCreated(t.token);
      setName("");
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
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Settings</h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">Account · API access</p>
      </div>

      <div className="flex max-w-3xl flex-col gap-6 px-9 py-8">
        <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-semibold text-ink">API tokens</h2>
            <p className="text-sm leading-relaxed text-graphite">
              Call the API with a token as{" "}
              <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">Authorization: Bearer obt_…</code>.
              A token authenticates as your account and inherits your role.
            </p>
          </div>

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
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Token name
              </span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && name.trim()) create.mutate();
                }}
                placeholder="e.g. ci-bot, laptop, filing script"
                className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
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
          {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}

          <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
            {tokens.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
            {tokens.isSuccess && items.length === 0 && (
              <div className="p-4 text-sm text-graphite-soft">No tokens yet.</div>
            )}
            {items.map((t) => (
              <div key={t.id} className="flex items-center gap-3 p-3.5">
                <div className="grow">
                  <div className="text-sm font-medium text-ink">{t.name}</div>
                  <div className="font-mono text-xs text-graphite-soft">
                    created {timeAgo(t.created_at)} ·{" "}
                    {t.last_used_at ? `last used ${timeAgo(t.last_used_at)}` : "never used"}
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
        </section>

        {canManageRoles && (
          <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
            <div className="flex flex-col gap-1">
              <h2 className="text-base font-semibold text-ink">Members</h2>
              <p className="text-sm leading-relaxed text-graphite">
                Roles set what each user can do. <span className="text-ink">Owner/admin</span> manage everything
                including roles; <span className="text-ink">maintainer</span> manages projects;{" "}
                <span className="text-ink">member</span> files &amp; works issues;{" "}
                <span className="text-ink">reporter</span> only reports; <span className="text-ink">bot</span> is for
                automation.
              </p>
            </div>
            <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
              {users.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
              {users.data?.items.map((u: User) => (
                <div key={u.id} className="flex items-center gap-3 p-3.5">
                  <Avatar user={u} size={28} />
                  <div className="min-w-0 grow">
                    <div className="truncate text-sm font-medium text-ink">
                      {u.display_name || u.email}
                      {u.id === me.data?.id && (
                        <span className="ml-2 text-xs font-normal text-graphite-soft">(you)</span>
                      )}
                    </div>
                    <div className="truncate text-xs text-graphite-soft">{u.email}</div>
                  </div>
                  <RoleSelect
                    value={u.role ?? "member"}
                    disabled={u.id === me.data?.id || setUserRole.isPending}
                    onChange={(role) => setUserRole.mutate({ id: u.id, role })}
                  />
                </div>
              ))}
            </div>
            {setUserRole.isError && <p className="text-sm text-critical">{(setUserRole.error as Error).message}</p>}
          </section>
        )}
      </div>
    </div>
  );
}

function RoleSelect({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (role: string) => void;
  disabled?: boolean;
}) {
  return (
    <select
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      aria-label="Role"
      title={disabled ? "You can't change your own role" : "Change role"}
      className="shrink-0 rounded-md border border-hairline bg-paper px-2.5 py-1.5 text-sm capitalize text-ink outline-none focus:border-blueprint disabled:opacity-60"
    >
      {ROLES.map((r) => (
        <option key={r} value={r}>
          {r}
        </option>
      ))}
    </select>
  );
}
