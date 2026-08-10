import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type User } from "../../../lib/api";
import { Avatar } from "../../../components/Badges";
import { Card, ErrorLine } from "../ui";

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

  return (
    <Card
      title="Members"
      description={
        <>
          Roles set what each user can do. <span className="text-ink">Owner/admin</span> manage everything including
          roles; <span className="text-ink">maintainer</span> manages projects; <span className="text-ink">member</span>{" "}
          files &amp; works issues; <span className="text-ink">reporter</span> only reports;{" "}
          <span className="text-ink">bot</span> is for automation.
        </>
      }
    >
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
