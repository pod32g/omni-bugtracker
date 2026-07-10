import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type Release } from "../../lib/api";
import { useProject } from "../../lib/project";
import { timeAgo } from "../../lib/activity";
import { IconPlus, IconTag } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function Releases() {
  const qc = useQueryClient();
  const { projectKey } = useProject();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");

  const releases = useQuery({
    queryKey: ["releases", projectKey],
    queryFn: () => api.listReleases(projectKey),
    enabled: !!projectKey,
  });

  const [version, setVersion] = useState("");
  const [name, setName] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["releases", projectKey] });
  const create = useMutation({
    mutationFn: () => api.createRelease(projectKey, { version: version.trim(), name: name.trim() }),
    onSuccess: () => {
      setVersion("");
      setName("");
      invalidate();
    },
  });
  const publish = useMutation({
    mutationFn: (id: string) => api.updateRelease(id, { state: "published" }),
    onSuccess: invalidate,
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteRelease(id), onSuccess: invalidate });

  const items = releases.data?.items ?? [];

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <h1 className="text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">Releases</h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
          {projectKey} · {items.filter((r) => r.state === "draft").length} in draft
        </p>
      </div>

      <div className="flex max-w-3xl flex-col gap-6 px-9 py-8">
        {canManage && (
          <div className="flex items-end gap-3 rounded-lg border border-hairline bg-paper p-4">
            <label className="w-36 text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Version
              </span>
              <input
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                placeholder="2.1.0"
                className="w-full rounded-md border border-hairline bg-paper px-3 py-2 font-mono text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
            <label className="grow text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Name (optional)
              </span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && version.trim()) create.mutate();
                }}
                placeholder="e.g. Summer release"
                className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
            <button
              disabled={!version.trim() || create.isPending}
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
          {releases.isLoading && <div className="text-sm text-graphite">Loading…</div>}
          {releases.isSuccess && items.length === 0 && (
            <div className="rounded-lg border border-dashed border-hairline p-8 text-center text-sm text-graphite-soft">
              No releases yet{canManage ? " — draft the first one above." : "."}
            </div>
          )}
          {items.map((r) => (
            <ReleaseCard
              key={r.id}
              r={r}
              canManage={canManage}
              onPublish={() => {
                if (r.open_issues === 0 || window.confirm(`${r.version} still has ${r.open_issues} open issue(s). Publish anyway?`))
                  publish.mutate(r.id);
              }}
              onDelete={() => {
                if (window.confirm(`Delete release ${r.version}? Its issues are kept (release unset).`)) del.mutate(r.id);
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function ReleaseCard({
  r,
  canManage,
  onPublish,
  onDelete,
}: {
  r: Release;
  canManage: boolean;
  onPublish: () => void;
  onDelete: () => void;
}) {
  const published = r.state === "published";
  return (
    <div className={`flex flex-col gap-2.5 rounded-lg border border-hairline bg-paper p-5 ${published ? "opacity-80" : ""}`}>
      <div className="flex items-center gap-3">
        <span className="grid h-7 w-7 shrink-0 place-items-center rounded-md bg-panel text-graphite">
          <IconTag size={14} />
        </span>
        <Link
          to={`/issues?filter=${encodeURIComponent(`release:${r.id}`)}`}
          className="font-mono text-base font-semibold text-ink transition hover:text-blueprint"
        >
          {r.version}
        </Link>
        {r.name && <span className="text-sm text-graphite">{r.name}</span>}
        <span
          className={`rounded-full px-2 py-0.5 font-mono text-[10px] font-medium uppercase tracking-caps ${
            published ? "bg-resolved-soft text-resolved" : "bg-panel text-graphite"
          }`}
        >
          {r.state}
        </span>
        <span className="grow" />
        {canManage && !published && (
          <button
            onClick={onPublish}
            className="shrink-0 rounded-md bg-blueprint px-3 py-1.5 text-sm font-semibold text-paper transition hover:opacity-90"
          >
            Publish
          </button>
        )}
        {canManage && (
          <button
            onClick={onDelete}
            className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical"
          >
            Delete
          </button>
        )}
      </div>
      <div className="flex items-center gap-3 font-mono text-xs text-graphite-soft">
        <span>
          {r.done_issues} done · {r.open_issues} open
        </span>
        {r.git_tag && <span>tag {r.git_tag}</span>}
        {published && r.released_at && <span>released {timeAgo(r.released_at)}</span>}
      </div>
    </div>
  );
}
