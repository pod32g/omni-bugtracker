import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Milestone } from "../../lib/api";
import { useProject } from "../../lib/project";
import { IconPlus } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function Milestones() {
  const qc = useQueryClient();
  const { projectKey } = useProject();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");

  const milestones = useQuery({
    queryKey: ["milestones", projectKey],
    queryFn: () => api.listMilestones(projectKey),
    enabled: !!projectKey,
  });

  const [title, setTitle] = useState("");
  const [dueOn, setDueOn] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["milestones", projectKey] });
  const create = useMutation({
    mutationFn: () => api.createMilestone(projectKey, { title: title.trim(), due_on: dueOn || undefined }),
    onSuccess: () => {
      setTitle("");
      setDueOn("");
      invalidate();
    },
  });
  const setState = useMutation({
    mutationFn: ({ id, state }: { id: string; state: "open" | "closed" }) => api.updateMilestone(id, { state }),
    onSuccess: invalidate,
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteMilestone(id), onSuccess: invalidate });

  const items = milestones.data?.items ?? [];

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Milestones</h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
          {projectKey} · {items.filter((m) => m.state === "open").length} open
        </p>
      </div>

      <div className="flex max-w-3xl flex-col gap-6 px-9 py-8">
        {canManage && (
          <div className="flex items-end gap-3 rounded-lg border border-hairline bg-paper p-4">
            <label className="grow text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                New milestone
              </span>
              <input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && title.trim()) create.mutate();
                }}
                placeholder="e.g. v1.1, Beta launch"
                className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
            <label className="text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Due (optional)
              </span>
              <input
                type="date"
                value={dueOn}
                onChange={(e) => setDueOn(e.target.value)}
                className="rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint"
              />
            </label>
            <button
              disabled={!title.trim() || create.isPending}
              onClick={() => create.mutate()}
              className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
            >
              <IconPlus size={15} />
              Add
            </button>
          </div>
        )}
        {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}

        <div className="flex flex-col gap-3">
          {milestones.isLoading && <div className="text-sm text-graphite">Loading…</div>}
          {milestones.isSuccess && items.length === 0 && (
            <div className="rounded-lg border border-dashed border-hairline p-8 text-center text-sm text-graphite-soft">
              No milestones yet{canManage ? " — create the first one above." : "."}
            </div>
          )}
          {items.map((m) => (
            <MilestoneCard
              key={m.id}
              m={m}
              canManage={canManage}
              onToggle={() => setState.mutate({ id: m.id, state: m.state === "open" ? "closed" : "open" })}
              onDelete={() => {
                if (window.confirm(`Delete milestone “${m.title}”? Its issues are kept (milestone unset).`))
                  del.mutate(m.id);
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function MilestoneCard({
  m,
  canManage,
  onToggle,
  onDelete,
}: {
  m: Milestone;
  canManage: boolean;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const total = m.open_issues + m.closed_issues;
  const pct = total === 0 ? 0 : Math.round((m.closed_issues / total) * 100);
  // Compare as ISO date strings — parsing "YYYY-MM-DD" with Date() lands on UTC
  // midnight and shifts a day in western timezones.
  const dueStr = m.due_on ? m.due_on.slice(0, 10) : null;
  const overdue = m.state === "open" && dueStr && dueStr < new Date().toISOString().slice(0, 10);

  return (
    <div className={`flex flex-col gap-3 rounded-lg border border-hairline bg-paper p-5 ${m.state === "closed" ? "opacity-70" : ""}`}>
      <div className="flex items-center gap-3">
        <Link
          to={`/issues?filter=${encodeURIComponent(`milestone:${m.id}`)}`}
          className="text-base font-semibold text-ink transition hover:text-blueprint"
        >
          {m.title}
        </Link>
        <span
          className={`rounded-full px-2 py-0.5 font-mono text-[10px] font-medium uppercase tracking-caps ${
            m.state === "open" ? "bg-blueprint-soft text-blueprint" : "bg-panel text-graphite"
          }`}
        >
          {m.state}
        </span>
        {dueStr && (
          <span className={`font-mono text-xs ${overdue ? "font-semibold text-critical" : "text-graphite-soft"}`}>
            due {dueStr}
            {overdue ? " · overdue" : ""}
          </span>
        )}
        <span className="grow" />
        {canManage && (
          <>
            <button
              onClick={onToggle}
              className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-graphite transition hover:border-graphite hover:text-ink"
            >
              {m.state === "open" ? "Close" : "Reopen"}
            </button>
            <button
              onClick={onDelete}
              className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical"
            >
              Delete
            </button>
          </>
        )}
      </div>
      <div className="flex items-center gap-3">
        <div className="h-1.5 grow overflow-hidden rounded-full bg-panel">
          <div className="h-full rounded-full bg-resolved transition-all" style={{ width: `${pct}%` }} />
        </div>
        <span className="shrink-0 font-mono text-xs text-graphite-soft">
          {m.closed_issues}/{total} done · {pct}%
        </span>
      </div>
    </div>
  );
}
