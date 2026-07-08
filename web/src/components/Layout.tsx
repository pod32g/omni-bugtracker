import type { ReactNode } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError, session } from "../lib/api";

const nav = [
  { to: "/", label: "Dashboard", end: true },
  { to: "/issues", label: "Issues", end: false },
];

export function Layout() {
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api.me(),
    retry: false,
  });

  if (me.isLoading) {
    return <Centered>Loading…</Centered>;
  }

  const unauthenticated = me.isError && (me.error as ApiError).status === 401;
  if (unauthenticated) {
    return <SignIn />;
  }
  if (me.isError) {
    return <Centered>API error: {(me.error as Error).message}</Centered>;
  }

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 shrink-0 flex-col border-r border-surface-border bg-surface-raised p-4">
        <div className="mb-6 flex items-center gap-2">
          <span className="grid h-8 w-8 place-items-center rounded-lg bg-accent font-bold text-white">O</span>
          <span className="font-semibold">Omni-BugTracker</span>
        </div>
        <nav className="space-y-1">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                `block rounded-lg px-3 py-2 text-sm transition ${
                  isActive
                    ? "bg-accent/15 text-accent-hover"
                    : "text-slate-400 hover:bg-white/5 hover:text-slate-200"
                }`
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto border-t border-surface-border pt-4 text-sm">
          <div className="truncate text-slate-300">{me.data?.display_name || me.data?.email || "Signed in"}</div>
          <div className="truncate text-xs text-slate-500">{me.data?.email}</div>
          <button
            onClick={() => session.logout()}
            className="mt-2 text-xs text-slate-400 hover:text-accent-hover"
          >
            Sign out
          </button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto p-8">
        <Outlet />
      </main>
    </div>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return <div className="grid min-h-screen place-items-center text-slate-400">{children}</div>;
}

function SignIn() {
  return (
    <div className="grid min-h-screen place-items-center">
      <div className="w-80 rounded-2xl border border-surface-border bg-surface-raised p-8 text-center">
        <div className="mx-auto mb-4 grid h-12 w-12 place-items-center rounded-xl bg-accent text-xl font-bold text-white">
          O
        </div>
        <h1 className="text-lg font-semibold">Omni-BugTracker</h1>
        <p className="mt-1 mb-6 text-sm text-slate-400">Sign in with your Omni account to continue.</p>
        <button
          onClick={() => session.login()}
          className="w-full rounded-lg bg-accent px-4 py-2 font-medium text-white hover:bg-accent-hover"
        >
          Sign in with Omni-Identity
        </button>
      </div>
    </div>
  );
}
