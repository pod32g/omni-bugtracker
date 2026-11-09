import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type User } from "../../../lib/api";
import { Avatar } from "../../../components/Badges";
import { IconPlus } from "../../../components/icons";
import { Card, ErrorLine, fieldLabelClass, inputClass } from "../ui";

const ROLES = ["owner", "admin", "maintainer", "member", "reporter", "bot"];

export function MembersSection() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  // Deactivated accounts are included here and nowhere else: this is the only screen
  // that can bring one back.
  const users = useQuery({ queryKey: ["users", "admin"], queryFn: () => api.listUsers(true) });
  const setUserRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => api.updateUserRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
  const setActive = useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) => api.setUserActive(id, isActive),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  // Local users: people who exist only in this tracker. Everyone else appears here on
  // their first sign-in; these are added by hand so they can be put on projects and
  // assigned work without (or before) an Omni-Identity login.
  const [newName, setNewName] = useState("");
  const [newEmail, setNewEmail] = useState("");
  const [newRole, setNewRole] = useState("member");
  const canCreate = newName.trim() !== "" && newEmail.trim() !== "";
  const createUser = useMutation({
    mutationFn: () => api.createUser({ display_name: newName.trim(), email: newEmail.trim(), role: newRole }),
    onSuccess: () => {
      setNewName("");
      setNewEmail("");
      setNewRole("member");
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });
  const submitCreate = () => {
    if (canCreate && !createUser.isPending) createUser.mutate();
  };

  return (
    <Card
      title="Members"
      description={
        <>
          Roles set what each user can do. <span className="text-ink">Owner/admin</span> manage everything including
          roles; <span className="text-ink">maintainer</span> manages projects; <span className="text-ink">member</span>{" "}
          files &amp; works issues; <span className="text-ink">reporter</span> only reports;{" "}
          <span className="text-ink">bot</span> is for automation. People appear here when they first sign in;{" "}
          <span className="text-ink">add user</span> creates a local account that can be assigned work but can&apos;t
          sign in.
        </>
      }
    >
      <div className="flex flex-wrap items-end gap-3">
        <label className="min-w-40 grow text-sm">
          <span className={fieldLabelClass}>Name</span>
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submitCreate()}
            placeholder="e.g. Pat Rivera"
            className={`w-full ${inputClass}`}
          />
        </label>
        <label className="min-w-48 grow text-sm">
          <span className={fieldLabelClass}>Email</span>
          <input
            type="email"
            value={newEmail}
            onChange={(e) => setNewEmail(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submitCreate()}
            placeholder="a placeholder is fine, e.g. pat@local.invalid"
            className={`w-full ${inputClass}`}
          />
        </label>
        <label className="text-sm">
          <span className={fieldLabelClass}>Role</span>
          <select
            value={newRole}
            onChange={(e) => setNewRole(e.target.value)}
            aria-label="Role for the new user"
            className={`capitalize ${inputClass}`}
          >
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          disabled={!canCreate || createUser.isPending}
          onClick={submitCreate}
          className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          <IconPlus size={15} />
          {createUser.isPending ? "Adding…" : "Add user"}
        </button>
      </div>
      <ErrorLine error={createUser.error} />
      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {users.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {users.data?.items.map((u: User) => {
          const deactivated = u.is_active === false;
          const isMe = u.id === me.data?.id;
          return (
            <div key={u.id} className={`flex items-center gap-3 p-3.5 ${deactivated ? "bg-paper-sunk" : ""}`}>
              <div className={deactivated ? "opacity-50" : undefined}>
                <Avatar user={u} size={28} />
              </div>
              <div className="min-w-0 grow">
                <div className="truncate text-sm font-medium text-ink">
                  {u.display_name || u.email}
                  {isMe && <span className="ml-2 text-xs font-normal text-graphite-soft">(you)</span>}
                  {deactivated && (
                    <span className="ml-2 rounded-full border border-hairline px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                      deactivated
                    </span>
                  )}
                  {u.is_local && (
                    <span
                      title="Created here by an admin. No Omni-Identity login, so this account can be assigned and mentioned but can't sign in."
                      className="ml-2 rounded-full border border-hairline px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-caps text-graphite-soft"
                    >
                      local · can&apos;t sign in
                    </span>
                  )}
                </div>
                <div className="truncate text-xs text-graphite-soft">{u.email}</div>
              </div>
              <RoleSelect
                value={u.role ?? "member"}
                disabled={isMe || deactivated || setUserRole.isPending}
                onChange={(role) => setUserRole.mutate({ id: u.id, role })}
              />
              <button
                type="button"
                disabled={isMe || setActive.isPending}
                onClick={() => setActive.mutate({ id: u.id, isActive: deactivated })}
                title={
                  isMe
                    ? "You can't deactivate yourself"
                    : deactivated
                      ? "Restore access for this user"
                      : "Revoke this user's access and API tokens. Their issues and comments stay."
                }
                className="shrink-0 rounded-md border border-hairline px-2.5 py-1.5 text-sm text-graphite outline-none hover:text-ink focus-visible:border-blueprint disabled:opacity-40"
              >
                {deactivated ? "Reactivate" : "Deactivate"}
              </button>
            </div>
          );
        })}
      </div>
      <ErrorLine error={setUserRole.error} />
      <ErrorLine error={setActive.error} />
    </Card>
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
