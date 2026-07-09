import type { ReactNode } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError, session, type Project, type User } from "../lib/api";
import { ProjectProvider, useProject } from "../lib/project";
import { useTheme } from "../lib/theme";
import { Avatar } from "./Badges";
import {
  IconBoard,
  IconChevronDown,
  IconDashboard,
  IconFlag,
  IconLogout,
  IconMark,
  IconMoon,
  IconSun,
  IconTag,
  IconTarget,
} from "./icons";

export function Layout() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });

  if (me.isLoading) return <Centered>Loading…</Centered>;

  const unauthenticated = me.isError && (me.error as ApiError).status === 401;
  if (unauthenticated) return <SignIn />;
  if (me.isError) return <Centered>API error: {(me.error as Error).message}</Centered>;

  return (
    <ProjectProvider>
      {/* App shell: fixed viewport, sidebar stays put, only <main> scrolls (its content
          grows internally instead of growing the whole page). */}
      <div className="flex h-screen overflow-hidden bg-paper">
        <Sidebar me={me.data} />
        <main className="min-w-0 flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
    </ProjectProvider>
  );
}

function Sidebar({ me }: { me?: User }) {
  const { projects, projectKey, current, setProjectKey } = useProject();
  const { theme, toggle } = useTheme();
  // Shares the ["dashboard"] cache with the Dashboard page — the open count is free here.
  const overview = useQuery({ queryKey: ["dashboard"], queryFn: () => api.dashboard(), retry: false });
  const openCount = overview.data?.open_issues;

  return (
    <aside className="flex w-[248px] shrink-0 flex-col border-r border-hairline bg-panel px-4 py-6">
      <div className="flex items-center gap-2.5 px-2 pt-1">
        <span className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-lg bg-blueprint text-paper">
          <IconMark size={20} />
        </span>
        <div className="flex flex-col">
          <span className="text-base font-bold leading-tight tracking-[-0.01em] text-ink">Omni BugTracker</span>
          <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">issues · v1.0</span>
        </div>
      </div>

      <ProjectSwitcher projects={projects} current={current} projectKey={projectKey} onChange={setProjectKey} />

      <nav className="mt-6 flex grow flex-col gap-0.5">
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
        <NavItemSoon icon={<IconFlag size={17} />} label="Milestones" />
        <NavItemSoon icon={<IconTag size={17} />} label="Releases" />
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

function NavItemSoon({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <div
      title="Coming soon"
      className="flex cursor-default items-center gap-2.5 rounded-md px-2.5 py-2 font-medium text-graphite/70"
    >
      <span className="grid h-[18px] w-[18px] place-items-center">{icon}</span>
      <span className="grow text-sm">{label}</span>
    </div>
  );
}

function ProjectSwitcher({
  projects,
  current,
  projectKey,
  onChange,
}: {
  projects: Project[];
  current?: Project;
  projectKey: string;
  onChange: (key: string) => void;
}) {
  return (
    <div className="relative mt-7">
      <div className="flex items-center justify-between rounded-md border border-hairline bg-paper px-3 py-2.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="rounded-sm bg-blueprint-soft px-1.5 py-0.5 font-mono text-xs font-semibold text-blueprint">
            {current?.key ?? "—"}
          </span>
          <span className="truncate text-sm font-medium text-ink">{current?.name ?? "No project"}</span>
        </div>
        <IconChevronDown size={14} className="text-graphite" />
      </div>
      {projects.length > 0 && (
        <select
          value={projectKey}
          onChange={(e) => onChange(e.target.value)}
          aria-label="Switch project"
          className="absolute inset-0 cursor-pointer opacity-0"
        >
          {projects.map((p) => (
            <option key={p.key} value={p.key}>
              {p.key} — {p.name}
            </option>
          ))}
        </select>
      )}
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
