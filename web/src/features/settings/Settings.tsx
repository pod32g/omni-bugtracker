import { useQuery } from "@tanstack/react-query";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { api } from "../../lib/api";

// Settings used to be one scrolling column of eight cards, which worked while there
// were three. Sections are routed (/settings/<slug>) so a link goes to the thing
// rather than to the top of everything, and so the page only fetches what it shows —
// the operations poll and the audit query no longer run for someone editing their
// notification preferences.

type Access = "all" | "manage" | "admin";

export interface SettingsSection {
  slug: string;
  label: string;
  group: string;
  access: Access;
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { slug: "profile", label: "Profile", group: "Account", access: "all" },
  { slug: "notifications", label: "Notifications", group: "Account", access: "all" },
  { slug: "tokens", label: "API tokens", group: "Account", access: "all" },
  { slug: "views", label: "Saved views", group: "Account", access: "all" },
  { slug: "labels", label: "Labels", group: "Workspace", access: "manage" },
  { slug: "automation", label: "Automation", group: "Workspace", access: "manage" },
  { slug: "webhooks", label: "Webhooks", group: "Workspace", access: "manage" },
  { slug: "members", label: "Members", group: "Workspace", access: "admin" },
  { slug: "integrations", label: "Integrations", group: "Operations", access: "admin" },
  { slug: "archive", label: "Auto-archive", group: "Operations", access: "admin" },
  { slug: "operations", label: "Operations", group: "Operations", access: "admin" },
  { slug: "audit", label: "Audit log", group: "Operations", access: "admin" },
];

const MANAGE_ROLES = ["owner", "admin", "maintainer"];
const ADMIN_ROLES = ["owner", "admin"];

export function canAccess(section: SettingsSection, role?: string): boolean {
  switch (section.access) {
    case "admin":
      return ADMIN_ROLES.includes(role ?? "");
    case "manage":
      return MANAGE_ROLES.includes(role ?? "");
    default:
      return true;
  }
}

export function Settings() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const role = me.data?.role;
  const { pathname } = useLocation();

  const visible = SETTINGS_SECTIONS.filter((s) => canAccess(s, role));
  const groups = visible.reduce<Record<string, SettingsSection[]>>((acc, s) => {
    (acc[s.group] ??= []).push(s);
    return acc;
  }, {});
  const current = visible.find((s) => pathname === `/settings/${s.slug}`);

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-4 pb-5 pt-7 backdrop-blur md:px-9">
        <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Settings</h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
          {current ? `${current.group} · ${current.label}` : "Account · Workspace · Operations"}
        </p>
      </div>

      <div className="flex flex-col gap-6 px-4 py-8 md:flex-row md:gap-9 md:px-9">
        {/* On a phone the rail becomes a scrolling strip of links rather than a
            column that pushes the actual settings off the first screen. */}
        <nav className="-mx-4 shrink-0 overflow-x-auto px-4 md:sticky md:top-[104px] md:mx-0 md:w-48 md:self-start md:overflow-visible md:px-0">
          <div className="flex gap-1.5 md:flex-col md:gap-0.5">
            {Object.entries(groups).map(([group, sections]) => (
              <div key={group} className="flex gap-1.5 md:flex-col md:gap-0.5">
                <div className="hidden px-2 pb-1 pt-3 font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft first:pt-0 md:block">
                  {group}
                </div>
                {sections.map((s) => (
                  <NavLink
                    key={s.slug}
                    to={s.slug}
                    className={({ isActive }) =>
                      `whitespace-nowrap rounded-md px-2.5 py-2 text-sm transition ${
                        isActive
                          ? "bg-blueprint-soft font-semibold text-blueprint"
                          : "font-medium text-graphite hover:bg-panel hover:text-ink"
                      }`
                    }
                  >
                    {s.label}
                  </NavLink>
                ))}
              </div>
            ))}
          </div>
        </nav>

        <div className="flex min-w-0 max-w-3xl grow flex-col gap-6">
          <Outlet />
        </div>
      </div>
    </div>
  );
}

/**
 * RequireAccess guards a section reached by URL rather than by the nav — the nav
 * already hides what you can't use, but a bookmark or a pasted link shouldn't render
 * an admin panel that then fails every request with a 403.
 */
export function RequireAccess({ section, children }: { section: string; children: React.ReactNode }) {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const meta = SETTINGS_SECTIONS.find((s) => s.slug === section);

  if (me.isLoading || !meta) return null;
  if (canAccess(meta, me.data?.role)) return <>{children}</>;

  return (
    <section className="flex flex-col gap-1 rounded-lg border border-hairline bg-paper p-6">
      <h2 className="text-base font-semibold text-ink">{meta.label}</h2>
      <p className="text-sm text-graphite">
        This section needs the {meta.access === "admin" ? "owner or admin" : "maintainer, admin or owner"} role. You're
        signed in as {me.data?.role ?? "unknown"}.
      </p>
    </section>
  );
}
