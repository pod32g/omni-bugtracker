import { useEffect, useState, type ReactNode } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, session, type Project, type User } from "../lib/api";
import { ProjectProvider, useProject } from "../lib/project";
import { useTheme } from "../lib/theme";
import { Avatar } from "./Badges";
import { SearchPalette } from "./SearchPalette";
import {
  IconBoard,
  IconChevronDown,
  IconDashboard,
  IconFlag,
  IconGear,
  IconLogout,
  IconMark,
  IconMoon,
  IconPlus,
  IconSearch,
  IconSun,
  IconTag,
  IconTarget,
} from "./icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function Layout() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });
  const [searchOpen, setSearchOpen] = useState(false);

  // Global search shortcuts: ⌘K / Ctrl-K anywhere, "/" outside form fields.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen(true);
      } else if (e.key === "/") {
        // The issues list binds "/" to its own filter box — don't fight it there.
        if (window.location.pathname === "/issues") return;
        const t = e.target as HTMLElement;
        if (t.tagName !== "INPUT" && t.tagName !== "TEXTAREA" && t.tagName !== "SELECT" && !t.isContentEditable) {
          e.preventDefault();
          setSearchOpen(true);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  if (me.isLoading) return <Centered>Loading…</Centered>;

  const unauthenticated = me.isError && (me.error as ApiError).status === 401;
  if (unauthenticated) return <SignIn />;
  if (me.isError) return <Centered>API error: {(me.error as Error).message}</Centered>;

  return (
    <ProjectProvider>
      {/* App shell: fixed viewport, sidebar stays put, only <main> scrolls (its content
          grows internally instead of growing the whole page). */}
      <div className="flex h-screen overflow-hidden bg-mist">
        <Sidebar me={me.data} onSearch={() => setSearchOpen(true)} />
        <main className="min-w-0 flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
      <SearchPalette open={searchOpen} onClose={() => setSearchOpen(false)} />
    </ProjectProvider>
  );
}

function Sidebar({ me, onSearch }: { me?: User; onSearch: () => void }) {
  const { projects, projectKey, current, setProjectKey } = useProject();
  const { theme, toggle } = useTheme();
  const canManage = CAN_MANAGE.has(me?.role ?? "");
  // Shares the ["dashboard"] cache with the Dashboard page — the open count is free here.
  const overview = useQuery({ queryKey: ["dashboard"], queryFn: () => api.dashboard(), retry: false });
  const openCount = overview.data?.open_issues;

  return (
    <aside className="flex w-[248px] shrink-0 flex-col border-r border-hairline bg-mist px-4 py-6">
      <div className="flex items-center gap-2.5 px-2 pt-1">
        <span className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-lg bg-blueprint text-paper">
          <IconMark size={20} />
        </span>
        <div className="flex flex-col">
          <span className="text-base font-bold leading-tight tracking-[-0.01em] text-ink">Omni BugTracker</span>
          <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">issues · v1.0</span>
        </div>
      </div>

      <ProjectSwitcher
        projects={projects}
        current={current}
        projectKey={projectKey}
        onChange={setProjectKey}
        canManage={canManage}
      />

      <button
        onClick={onSearch}
        className="mt-4 flex items-center gap-2.5 rounded-md border border-hairline bg-paper px-2.5 py-2 text-graphite transition hover:border-graphite hover:text-ink"
      >
        <span className="grid h-[18px] w-[18px] place-items-center">
          <IconSearch size={16} />
        </span>
        <span className="grow text-left text-sm font-medium">Search</span>
        <kbd className="rounded border border-hairline bg-panel px-1.5 py-0.5 font-mono text-[10px] text-graphite-soft">
          ⌘K
        </kbd>
      </button>

      <nav className="mt-4 flex grow flex-col gap-0.5">
        <div className="px-2 pb-2 font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft">
          Workspace
        </div>
        <NavItem to="/" end icon={<IconDashboard size={17} />} label="Dashboard" />
        <NavItem
          to="/issues"
          icon={<IconTarget size={17} />}
          label="Issues"
          trailing={openCount != null ? String(openCount) : undefined}
        />
        <NavItem to="/board" icon={<IconBoard size={17} />} label="Board" />
        <NavItem to="/milestones" icon={<IconFlag size={17} />} label="Milestones" />
        <NavItem to="/releases" icon={<IconTag size={17} />} label="Releases" />
        <NavItem to="/settings" icon={<IconGear size={17} />} label="Settings" />
      </nav>

      <div className="mt-auto flex items-center gap-2.5 border-t border-hairline px-2.5 pb-1 pt-3">
        <Avatar user={me} size={32} />
        <div className="flex min-w-0 grow flex-col">
          <span className="truncate text-sm font-semibold leading-tight text-ink">
            {me?.display_name || me?.email || "Signed in"}
          </span>
          <span className="font-mono text-[10px] uppercase tracking-[0.1em] text-graphite-soft">
            {(me?.role ?? "member").toUpperCase()}
          </span>
        </div>
        <div className="flex items-center gap-0.5">
          <button
            onClick={toggle}
            title={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
            className="grid h-7 w-7 place-items-center rounded-md text-graphite transition hover:bg-paper hover:text-ink"
          >
            {theme === "dark" ? <IconMoon size={16} /> : <IconSun size={16} />}
          </button>
          <button
            onClick={() => session.logout()}
            title="Sign out"
            className="grid h-7 w-7 place-items-center rounded-md text-graphite transition hover:bg-paper hover:text-ink"
          >
            <IconLogout size={16} />
          </button>
        </div>
      </div>
    </aside>
  );
}

function NavItem({
  to,
  end,
  icon,
  label,
  trailing,
}: {
  to: string;
  end?: boolean;
  icon: ReactNode;
  label: string;
  trailing?: string;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        `flex items-center gap-2.5 rounded-md px-2.5 py-2 transition ${
          isActive
            ? "bg-blueprint-soft font-semibold text-blueprint"
            : "font-medium text-graphite hover:bg-paper hover:text-ink"
        }`
      }
    >
      <span className="grid h-[18px] w-[18px] place-items-center">{icon}</span>
      <span className="grow text-sm">{label}</span>
      {trailing != null && <span className="font-mono text-xs font-medium">{trailing}</span>}
    </NavLink>
  );
}

function ProjectSwitcher({
  projects,
  current,
  projectKey,
  onChange,
  canManage,
}: {
  projects: Project[];
  current?: Project;
  projectKey: string;
  onChange: (key: string) => void;
  canManage: boolean;
}) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  return (
    <div className="relative mt-7">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-label="Switch or create project"
        className="flex w-full items-center justify-between rounded-md border border-hairline bg-paper px-3 py-2.5 transition hover:border-graphite"
      >
        <span className="flex min-w-0 items-center gap-2.5">
          <span className="rounded-sm bg-blueprint-soft px-1.5 py-0.5 font-mono text-xs font-semibold text-blueprint">
            {current?.key ?? "—"}
          </span>
          <span className="truncate text-sm font-medium text-ink">{current?.name ?? "No project"}</span>
        </span>
        <IconChevronDown size={14} className="text-graphite" />
      </button>

      {open && (
        <>
          <button className="fixed inset-0 z-10 cursor-default" aria-hidden onClick={() => setOpen(false)} />
          <div className="absolute inset-x-0 top-[46px] z-20 max-h-72 overflow-y-auto rounded-md border border-hairline bg-paper py-1 shadow-lg shadow-ink/10">
            {projects.length === 0 && (
              <div className="px-3 py-2 text-xs text-graphite-soft">No projects yet.</div>
            )}
            {projects.map((p) => (
              <button
                key={p.key}
                onClick={() => {
                  onChange(p.key);
                  setOpen(false);
                }}
                className={`flex w-full items-center gap-2.5 px-3 py-2 text-left transition hover:bg-panel ${
                  p.key === projectKey ? "bg-blueprint-soft" : ""
                }`}
              >
                <span className="rounded-sm bg-blueprint-soft px-1.5 py-0.5 font-mono text-[11px] font-semibold text-blueprint">
                  {p.key}
                </span>
                <span className="truncate text-sm text-ink">{p.name}</span>
              </button>
            ))}
            {canManage && (
              <>
                {projects.length > 0 && <div className="my-1 border-t border-hairline" />}
                {current && (
                  <button
                    onClick={() => {
                      setOpen(false);
                      navigate(`/projects/${current.key}/settings`);
                    }}
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium text-graphite transition hover:bg-panel hover:text-ink"
                  >
                    <IconGear size={14} />
                    Project settings
                  </button>
                )}
                <button
                  onClick={() => {
                    setOpen(false);
                    setCreating(true);
                  }}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium text-blueprint transition hover:bg-panel"
                >
                  <IconPlus size={14} />
                  New project
                </button>
              </>
            )}
          </div>
        </>
      )}

      {creating && (
        <NewProjectDialog
          onClose={() => setCreating(false)}
          onCreated={(project) => {
            // Seed the cache so the context's default-selection effect doesn't bounce
            // the selection back to the first project before the refetch lands.
            qc.setQueryData<{ items: Project[] }>(["projects"], (old) => ({
              items: [...(old?.items ?? []), project],
            }));
            onChange(project.key);
            setCreating(false);
          }}
        />
      )}
    </div>
  );
}

function NewProjectDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (project: Project) => void }) {
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => api.createProject({ key: key.toUpperCase(), name: name.trim() }),
    onSuccess: (p) => onCreated(p),
  });
  const valid = /^[A-Z][A-Z0-9]{1,9}$/.test(key) && name.trim().length > 0;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-ink/40 p-4" onClick={onClose}>
      <div
        className="w-full max-w-sm rounded-lg border border-hairline bg-paper p-6 shadow-xl shadow-ink/10"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-lg font-bold text-ink">New project</h2>
        <label className="mb-3 block">
          <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Key</span>
          <input
            autoFocus
            value={key}
            onChange={(e) => setKey(e.target.value.toUpperCase())}
            placeholder="API"
            maxLength={10}
            className="w-full rounded-md border border-hairline bg-paper px-3 py-2 font-mono uppercase text-ink outline-none focus:border-blueprint"
          />
          <span className="mt-1 block text-xs text-graphite-soft">
            2–10 uppercase letters/digits; the issue prefix (e.g. API-1).
          </span>
        </label>
        <label className="mb-4 block">
          <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && valid && !create.isPending) create.mutate();
            }}
            placeholder="API Service"
            className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-ink outline-none focus:border-blueprint"
          />
        </label>
        {create.isError && <p className="mb-3 text-sm text-critical">{(create.error as Error).message}</p>}
        <div className="flex justify-end gap-2">
          <button onClick={onClose} className="rounded-md px-4 py-2 text-sm text-graphite hover:text-ink">
            Cancel
          </button>
          <button
            disabled={!valid || create.isPending}
            onClick={() => create.mutate()}
            className="rounded-md bg-blueprint px-4 py-2 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            {create.isPending ? "Creating…" : "Create project"}
          </button>
        </div>
      </div>
    </div>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return <div className="grid min-h-screen place-items-center bg-paper text-graphite">{children}</div>;
}

function SignIn() {
  return (
    <div className="grid min-h-screen place-items-center bg-panel">
      <div className="w-80 rounded-lg border border-hairline bg-paper p-8 text-center">
        <div className="mx-auto mb-4 grid h-12 w-12 place-items-center rounded-lg bg-blueprint text-paper">
          <IconMark size={26} />
        </div>
        <h1 className="text-lg font-bold text-ink">Omni BugTracker</h1>
        <p className="mb-6 mt-1 text-sm text-graphite">Sign in with your Omni account to continue.</p>
        <button
          onClick={() => session.login()}
          className="w-full rounded-md bg-blueprint px-4 py-2 font-semibold text-paper transition hover:opacity-90"
        >
          Sign in with Omni-Identity
        </button>
      </div>
    </div>
  );
}
