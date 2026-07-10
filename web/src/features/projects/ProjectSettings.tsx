import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { api, UNASSIGNED, type Component } from "../../lib/api";
import { AssigneeSelect } from "../issues/formFields";
import { IconPlus } from "../../components/icons";

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function ProjectSettings() {
  const { key = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const canManage = CAN_MANAGE.has(me.data?.role ?? "");
  const project = useQuery({ queryKey: ["project", key], queryFn: () => api.getProject(key), enabled: !!key });

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [defaultAssignee, setDefaultAssignee] = useState(UNASSIGNED);
  const [saved, setSaved] = useState(false);

  // Seed the form once the project loads (and re-seed when switching projects).
  useEffect(() => {
    if (!project.data) return;
    setName(project.data.name);
    setDescription(project.data.description_md);
    setDefaultAssignee(project.data.default_assignee_id ?? UNASSIGNED);
  }, [project.data]);

  const save = useMutation({
    mutationFn: () =>
      api.updateProject(key, {
        name: name.trim(),
        description_md: description,
        default_assignee_id: defaultAssignee,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["project", key] });
      qc.invalidateQueries({ queryKey: ["projects"] });
      setSaved(true);
      setTimeout(() => setSaved(false), 1800);
    },
  });

  const archive = useMutation({
    mutationFn: () => api.archiveProject(key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      navigate("/");
    },
  });

  if (project.isLoading) return <div className="px-9 py-10 text-sm text-graphite">Loading…</div>;
  if (project.isError || !project.data)
    return <div className="px-9 py-10 text-sm text-critical">{(project.error as Error)?.message ?? "Not found"}</div>;

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-9 pb-5 pt-7 backdrop-blur">
        <h1 className="flex items-center gap-3 text-[30px] font-bold leading-none tracking-[-0.02em] text-ink">
          <span className="rounded-sm bg-blueprint-soft px-2 py-1 font-mono text-lg font-semibold text-blueprint">
            {key}
          </span>
          Project settings
        </h1>
        <p className="font-mono text-xs uppercase tracking-[0.06em] text-graphite">
          {project.data.name} · configuration
        </p>
      </div>

      <div className="flex max-w-3xl flex-col gap-6 px-9 py-8">
        {!canManage && (
          <p className="rounded-md border border-hairline bg-panel px-4 py-3 text-sm text-graphite">
            You need the maintainer role (or above) to change project settings.
          </p>
        )}

        <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-semibold text-ink">Details</h2>
            <p className="text-sm leading-relaxed text-graphite">
              The project key <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">{key}</code>{" "}
              is permanent — it prefixes every issue key.
            </p>
          </div>

          <label className="block">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Name</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={!canManage}
              className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint disabled:opacity-60"
            />
          </label>

          <label className="block">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Description
            </span>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              disabled={!canManage}
              placeholder="What is this project about? (Markdown supported)"
              className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint disabled:opacity-60"
            />
          </label>

          <label className="block">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Default assignee
            </span>
            <div className={canManage ? "" : "pointer-events-none opacity-60"}>
              <AssigneeSelect value={defaultAssignee} onChange={setDefaultAssignee} unassignedValue={UNASSIGNED} />
            </div>
            <span className="mt-1 block text-xs text-graphite-soft">
              New issues created without an assignee are assigned to this person.
            </span>
          </label>

          {save.isError && <p className="text-sm text-critical">{(save.error as Error).message}</p>}
          <div className="flex items-center gap-3">
            <button
              disabled={!canManage || !name.trim() || save.isPending}
              onClick={() => save.mutate()}
              className="rounded-md bg-blueprint px-4 py-2 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
            >
              {save.isPending ? "Saving…" : "Save changes"}
            </button>
            {saved && <span className="text-sm font-medium text-resolved">Saved.</span>}
          </div>
        </section>

        <ComponentsSection projectKey={key} canManage={canManage} />

        {canManage && (
          <section className="flex flex-col gap-3 rounded-lg border border-critical/40 bg-paper p-6">
            <div className="flex flex-col gap-1">
              <h2 className="text-base font-semibold text-critical">Danger zone</h2>
              <p className="text-sm leading-relaxed text-graphite">
                Archiving hides the project and its issues from pickers and lists. Nothing is deleted — an admin can
                unarchive via the API.
              </p>
            </div>
            {archive.isError && <p className="text-sm text-critical">{(archive.error as Error).message}</p>}
            <div>
              <button
                disabled={archive.isPending}
                onClick={() => {
                  if (window.confirm(`Archive ${key} — “${project.data?.name}”? It disappears from the project picker.`))
                    archive.mutate();
                }}
                className="rounded-md border border-critical px-4 py-2 text-sm font-semibold text-critical transition hover:bg-critical-soft disabled:opacity-50"
              >
                {archive.isPending ? "Archiving…" : "Archive project"}
              </button>
            </div>
          </section>
        )}
      </div>
    </div>
  );
}

function ComponentsSection({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const qc = useQueryClient();
  const components = useQuery({
    queryKey: ["components", projectKey],
    queryFn: () => api.listComponents(projectKey),
  });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const [newName, setNewName] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["components", projectKey] });
  const create = useMutation({
    mutationFn: () => api.createComponent(projectKey, { name: newName.trim() }),
    onSuccess: () => {
      setNewName("");
      invalidate();
    },
  });
  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: { name?: string; lead_id?: string } }) =>
      api.updateComponent(id, patch),
    onSuccess: invalidate,
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteComponent(id), onSuccess: invalidate });

  const items = components.data?.items ?? [];

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">Components</h2>
        <p className="text-sm leading-relaxed text-graphite">
          Areas of ownership (e.g. <span className="font-mono text-xs">api</span>,{" "}
          <span className="font-mono text-xs">web</span>). Issues can be tagged with components and filtered with{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">component:name</code>.
        </p>
      </div>

      {canManage && (
        <div className="flex items-end gap-3">
          <label className="grow text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              New component
            </span>
            <input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && newName.trim()) create.mutate();
              }}
              placeholder="e.g. api, web, infra"
              className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
            />
          </label>
          <button
            disabled={!newName.trim() || create.isPending}
            onClick={() => create.mutate()}
            className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            <IconPlus size={15} />
            Add
          </button>
        </div>
      )}
      {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}
      {update.isError && <p className="text-sm text-critical">{(update.error as Error).message}</p>}

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {components.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {components.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No components yet.</div>
        )}
        {items.map((c) => (
          <ComponentRow
            key={c.id}
            component={c}
            users={users.data?.items ?? []}
            canManage={canManage}
            onRename={(name) => update.mutate({ id: c.id, patch: { name } })}
            onLead={(lead_id) => update.mutate({ id: c.id, patch: { lead_id } })}
            onDelete={() => {
              if (window.confirm(`Delete component “${c.name}”? It is removed from all issues.`)) del.mutate(c.id);
            }}
          />
        ))}
      </div>
    </section>
  );
}

function ComponentRow({
  component,
  users,
  canManage,
  onRename,
  onLead,
  onDelete,
}: {
  component: Component;
  users: { id: string; display_name: string; email: string }[];
  canManage: boolean;
  onRename: (name: string) => void;
  onLead: (leadId: string) => void;
  onDelete: () => void;
}) {
  const [name, setName] = useState(component.name);
  useEffect(() => setName(component.name), [component.name]);

  return (
    <div className="flex items-center gap-3 p-3.5">
      <input
        value={name}
        disabled={!canManage}
        onChange={(e) => setName(e.target.value)}
        onBlur={() => {
          const n = name.trim();
          if (n && n !== component.name) onRename(n);
          else setName(component.name);
        }}
        className="w-40 rounded-md border border-transparent bg-transparent px-2 py-1 font-mono text-sm font-medium text-ink outline-none transition hover:border-hairline focus:border-blueprint disabled:opacity-100"
      />
      <span className="grow font-mono text-xs text-graphite-soft">
        {component.open_issues} open issue{component.open_issues === 1 ? "" : "s"}
      </span>
      <select
        value={component.lead_id ?? UNASSIGNED}
        disabled={!canManage}
        onChange={(e) => onLead(e.target.value)}
        aria-label="Component lead"
        className="shrink-0 rounded-md border border-hairline bg-paper px-2.5 py-1.5 text-sm text-ink outline-none focus:border-blueprint disabled:opacity-60"
      >
        <option value={UNASSIGNED}>No lead</option>
        {users.map((u) => (
          <option key={u.id} value={u.id}>
            {u.display_name || u.email}
          </option>
        ))}
      </select>
      {canManage && (
        <button
          onClick={onDelete}
          className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical"
        >
          Delete
        </button>
      )}
    </div>
  );
}
