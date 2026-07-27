import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  api,
  UNASSIGNED,
  type Component,
  type FieldType,
  type IssueType,
  type User,
} from "../../lib/api";
import { useProject } from "../../lib/project";
import { AssigneeSelect } from "../issues/formFields";
import { Avatar } from "../../components/Badges";
import { IconPlus } from "../../components/icons";

const ROLES = ["owner", "admin", "maintainer", "member", "reporter", "bot"];
const KEY_RE = /^[A-Z][A-Z0-9]{1,9}$/;

const CAN_MANAGE = new Set(["owner", "admin", "maintainer"]);

export function ProjectSettings() {
  const { key = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const { setProjectKey } = useProject();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const project = useQuery({ queryKey: ["project", key], queryFn: () => api.getProject(key), enabled: !!key });
  // my_role is the effective role: global role, elevated by project membership.
  const canManage = CAN_MANAGE.has(project.data?.my_role ?? me.data?.role ?? "");

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [defaultAssignee, setDefaultAssignee] = useState(UNASSIGNED);
  const [saved, setSaved] = useState(false);
  const [newKey, setNewKey] = useState("");

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

  const rename = useMutation({
    mutationFn: () => api.renameProjectKey(key, newKey.trim()),
    onSuccess: (updated) => {
      // Persist the new key, then hard-navigate so the project context and every
      // query cached under the old key re-initialize cleanly. (An in-SPA update
      // races the context's "is this key still in the list?" fallback, which would
      // otherwise bounce the selection to the first project.)
      setProjectKey(updated.key);
      window.location.assign(`/projects/${updated.key}/settings`);
    },
  });

  const archive = useMutation({
    mutationFn: () => api.archiveProject(key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      navigate("/");
    },
  });

  if (project.isLoading) return <div className="px-4 md:px-9 py-10 text-sm text-graphite">Loading…</div>;
  if (project.isError || !project.data)
    return <div className="px-4 md:px-9 py-10 text-sm text-critical">{(project.error as Error)?.message ?? "Not found"}</div>;

  return (
    <div>
      <div className="sticky top-0 z-10 flex flex-col gap-1.5 border-b border-hairline bg-paper/80 px-4 md:px-9 pb-5 pt-7 backdrop-blur">
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

      <div className="flex max-w-3xl flex-col gap-6 px-4 md:px-9 py-8">
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
              prefixes every issue key. It can be changed in the danger zone below.
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

        <TemplatesSection projectKey={key} canManage={canManage} />

        <FieldsSection projectKey={key} canManage={canManage} />

        <SLASection projectKey={key} canManage={canManage} />

        <MembersSection projectKey={key} canManage={canManage} />

        {canManage && (
          <section className="flex flex-col gap-5 rounded-lg border border-critical/40 bg-paper p-6">
            <h2 className="text-base font-semibold text-critical">Danger zone</h2>

            <div className="flex flex-col gap-2">
              <h3 className="text-sm font-semibold text-ink">Change project key</h3>
              <p className="text-sm leading-relaxed text-graphite">
                Re-labels every issue —{" "}
                <span className="font-mono text-ink">{key}-42</span> becomes{" "}
                <span className="font-mono text-ink">{(newKey.trim() || "NEW") + "-42"}</span>. Nothing is deleted, but
                links to the old keys elsewhere (git commits, bookmarks) will stop resolving.
              </p>
              {rename.isError && <p className="text-sm text-critical">{(rename.error as Error).message}</p>}
              <div className="flex items-end gap-3">
                <label className="text-sm">
                  <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                    New key
                  </span>
                  <input
                    value={newKey}
                    onChange={(e) => setNewKey(e.target.value.toUpperCase())}
                    placeholder={key}
                    maxLength={10}
                    className="w-44 rounded-md border border-hairline bg-paper px-3 py-2 font-mono text-sm uppercase text-ink outline-none focus:border-blueprint"
                  />
                </label>
                <button
                  disabled={rename.isPending || !KEY_RE.test(newKey.trim()) || newKey.trim() === key}
                  onClick={() => {
                    const nk = newKey.trim();
                    if (window.confirm(`Rename ${key} → ${nk}? Every issue key becomes ${nk}-<n>.`)) rename.mutate();
                  }}
                  className="h-10 rounded-md border border-critical px-4 text-sm font-semibold text-critical transition hover:bg-critical-soft disabled:opacity-50"
                >
                  {rename.isPending ? "Renaming…" : "Change key"}
                </button>
              </div>
            </div>

            <div className="flex flex-col gap-2 border-t border-hairline pt-5">
              <h3 className="text-sm font-semibold text-ink">Archive project</h3>
              <p className="text-sm leading-relaxed text-graphite">
                Hides the project and its issues from pickers and lists. Nothing is deleted — an admin can unarchive via
                the API.
              </p>
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

function TemplatesSection({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const qc = useQueryClient();
  const templates = useQuery({
    queryKey: ["templates", projectKey],
    queryFn: () => api.listIssueTemplates(projectKey),
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["templates", projectKey] });

  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [type, setType] = useState<IssueType>("bug");
  const [bodyMD, setBodyMD] = useState("");
  const [required, setRequired] = useState("");

  const reset = () => {
    setEditing(null);
    setName("");
    setBodyMD("");
    setRequired("");
  };
  const sections = () =>
    required
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

  const save = useMutation({
    mutationFn: () =>
      editing
        ? api.updateIssueTemplate(editing, {
            name: name.trim(),
            body_md: bodyMD,
            required_sections: sections(),
          })
        : api.createIssueTemplate(projectKey, {
            name: name.trim(),
            type,
            body_md: bodyMD,
            required_sections: sections(),
          }),
    onSuccess: () => {
      reset();
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteIssueTemplate(id), onSuccess: invalidate });
  const setDefault = useMutation({
    mutationFn: (id: string) => api.updateIssueTemplate(id, { is_default: true }),
    onSuccess: invalidate,
  });

  const items = templates.data?.items ?? [];

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">Issue templates</h2>
        <p className="text-sm leading-relaxed text-graphite">
          What a filed issue should say, per type. Triage cost is decided at filing time;
          a template moves it to the person with the most context. Required sections are
          markdown headings that must be present <em>and answered</em> — filing with the
          prompts still in place is rejected, naming the sections.
        </p>
        <p className="text-sm leading-relaxed text-graphite-soft">
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">- [ ]</code>{" "}
          lines become live checkboxes on the issue, and their progress shows in the list.
        </p>
      </div>

      {canManage && (
        <div className="flex flex-col gap-3 rounded-md border border-hairline bg-panel/50 p-3">
          <div className="flex flex-wrap items-end gap-3">
            <label className="grow text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Name
              </span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Regression"
                className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
            <label className="text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Type
              </span>
              <select
                value={type}
                disabled={!!editing}
                onChange={(e) => setType(e.target.value as IssueType)}
                className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink disabled:opacity-50"
              >
                {ISSUE_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </label>
            <button
              disabled={!name.trim() || save.isPending}
              onClick={() => save.mutate()}
              className="flex h-9 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
            >
              <IconPlus size={15} />
              {editing ? "Save" : "Add"}
            </button>
            {editing && (
              <button onClick={reset} className="h-9 text-sm font-semibold text-graphite transition hover:text-ink">
                Cancel
              </button>
            )}
          </div>
          <label className="text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Body (Markdown)
            </span>
            <textarea
              value={bodyMD}
              onChange={(e) => setBodyMD(e.target.value)}
              rows={8}
              placeholder={"## Steps to reproduce\n\n## Expected\n\n## Actual\n\n## Definition of done\n\n- [ ] fix\n- [ ] test"}
              className="w-full rounded-md border border-hairline bg-paper p-3 font-mono text-xs leading-relaxed text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
            />
          </label>
          <label className="text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Required sections (comma separated headings)
            </span>
            <input
              value={required}
              onChange={(e) => setRequired(e.target.value)}
              placeholder="Steps to reproduce, Expected, Actual"
              className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
            />
          </label>
          {save.isError && <p className="text-sm text-critical">{(save.error as Error).message}</p>}
        </div>
      )}

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {templates.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {templates.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">
            No templates — the create form falls back to a blank description.
          </div>
        )}
        {items.map((t) => (
          <div key={t.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <span className="text-sm font-medium text-ink">{t.name}</span>
            <span className="rounded-full border border-hairline bg-panel px-2 py-px text-xs capitalize text-graphite">
              {t.type}
            </span>
            {t.is_default && (
              <span className="rounded-full border border-blueprint-border bg-blueprint-soft px-2 py-px text-xs font-semibold text-blueprint">
                default
              </span>
            )}
            {t.required_sections.length > 0 && (
              <span className="text-xs text-graphite-soft">
                requires {t.required_sections.join(", ")}
              </span>
            )}
            {canManage && (
              <span className="ml-auto flex items-center gap-3 text-xs font-semibold">
                {!t.is_default && (
                  <button
                    onClick={() => setDefault.mutate(t.id)}
                    className="text-blueprint transition hover:opacity-80"
                  >
                    Make default
                  </button>
                )}
                <button
                  onClick={() => {
                    setEditing(t.id);
                    setName(t.name);
                    setType(t.type);
                    setBodyMD(t.body_md);
                    setRequired(t.required_sections.join(", "));
                  }}
                  className="text-blueprint transition hover:opacity-80"
                >
                  Edit
                </button>
                <button
                  onClick={() => {
                    if (window.confirm(`Delete the ${t.name} template?`)) del.mutate(t.id);
                  }}
                  className="text-graphite transition hover:text-critical"
                >
                  Delete
                </button>
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

const FIELD_TYPES: FieldType[] = [
  "text",
  "number",
  "select",
  "multi_select",
  "date",
  "user",
  "checkbox",
  "url",
];

const ISSUE_TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];

function FieldsSection({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const qc = useQueryClient();
  const defs = useQuery({
    queryKey: ["field-defs", projectKey],
    queryFn: () => api.listFieldDefinitions(projectKey),
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["field-defs", projectKey] });

  const [label, setLabel] = useState("");
  const [fieldKey, setFieldKey] = useState("");
  const [type, setType] = useState<FieldType>("text");
  const [options, setOptions] = useState("");
  const [required, setRequired] = useState(false);
  const [appliesTo, setAppliesTo] = useState<string[]>([]);
  // The key is derived from the label until somebody edits it directly — nobody wants
  // to type "customer_tier" by hand, and the key is what filters reference forever.
  const [keyTouched, setKeyTouched] = useState(false);

  const create = useMutation({
    mutationFn: () =>
      api.createFieldDefinition(projectKey, {
        key: fieldKey,
        label: label.trim(),
        type,
        options: options
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
        required,
        applies_to: appliesTo,
      }),
    onSuccess: () => {
      setLabel("");
      setFieldKey("");
      setOptions("");
      setRequired(false);
      setAppliesTo([]);
      setKeyTouched(false);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteFieldDefinition(id), onSuccess: invalidate });
  const update = useMutation({
    mutationFn: ({ id, required }: { id: string; required: boolean }) =>
      api.updateFieldDefinition(id, { required }),
    onSuccess: invalidate,
  });

  const needsOptions = type === "select" || type === "multi_select";
  const items = defs.data?.items ?? [];

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">Custom fields</h2>
        <p className="text-sm leading-relaxed text-graphite">
          Extra structure this project needs and the built-in shape does not have. Unlike
          labels, a field has a type, can be required, and can be filtered exactly:{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">
            field:customer_tier:enterprise
          </code>
          .
        </p>
        <p className="text-sm leading-relaxed text-graphite-soft">
          The key and type are fixed once created — the key is what saved searches
          reference, and a type change would strand every existing value.
        </p>
      </div>

      {canManage && (
        <div className="flex flex-col gap-3 rounded-md border border-hairline bg-panel/50 p-3">
          <div className="flex flex-wrap items-end gap-3">
            <label className="grow text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Label
              </span>
              <input
                value={label}
                onChange={(e) => {
                  setLabel(e.target.value);
                  if (!keyTouched) {
                    setFieldKey(
                      e.target.value
                        .toLowerCase()
                        .replace(/[^a-z0-9]+/g, "_")
                        .replace(/^_+|_+$/g, "")
                        .slice(0, 39),
                    );
                  }
                }}
                placeholder="Customer tier"
                className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
            <label className="text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Key
              </span>
              <input
                value={fieldKey}
                onChange={(e) => {
                  setKeyTouched(true);
                  setFieldKey(e.target.value);
                }}
                className="h-9 w-40 rounded-md border border-hairline bg-paper px-3 font-mono text-xs text-ink outline-none focus:border-blueprint"
              />
            </label>
            <label className="text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Type
              </span>
              <select
                value={type}
                onChange={(e) => setType(e.target.value as FieldType)}
                className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
              >
                {FIELD_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t.replace("_", " ")}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex h-9 items-center gap-2 text-sm text-ink">
              <input
                type="checkbox"
                checked={required}
                onChange={(e) => setRequired(e.target.checked)}
                className="accent-blueprint"
              />
              Required
            </label>
            <button
              disabled={!label.trim() || !fieldKey || (needsOptions && !options.trim()) || create.isPending}
              onClick={() => create.mutate()}
              className="flex h-9 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
            >
              <IconPlus size={15} />
              Add
            </button>
          </div>
          {needsOptions && (
            <label className="text-sm">
              <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                Options (comma separated)
              </span>
              <input
                value={options}
                onChange={(e) => setOptions(e.target.value)}
                placeholder="free, pro, enterprise"
                className="h-9 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
            </label>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Applies to
            </span>
            {ISSUE_TYPES.map((t) => {
              const on = appliesTo.includes(t);
              return (
                <button
                  key={t}
                  onClick={() => setAppliesTo(on ? appliesTo.filter((x) => x !== t) : [...appliesTo, t])}
                  className={`rounded-full border px-2.5 py-0.5 text-xs font-medium capitalize transition ${
                    on
                      ? "border-blueprint bg-blueprint-soft text-blueprint"
                      : "border-hairline text-graphite hover:border-graphite hover:text-ink"
                  }`}
                >
                  {t}
                </button>
              );
            })}
            {appliesTo.length === 0 && (
              <span className="text-xs text-graphite-soft">none selected = every type</span>
            )}
          </div>
          {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}
        </div>
      )}

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {defs.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {defs.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No custom fields.</div>
        )}
        {items.map((d) => (
          <div key={d.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <span className="text-sm font-medium text-ink">{d.label}</span>
            <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-graphite">{d.key}</code>
            <span className="text-xs text-graphite">{d.type.replace("_", " ")}</span>
            {d.options.length > 0 && (
              <span className="text-xs text-graphite-soft">{d.options.join(" · ")}</span>
            )}
            {d.applies_to.length > 0 && (
              <span className="text-xs text-graphite-soft">{d.applies_to.join(", ")} only</span>
            )}
            {canManage && (
              <span className="ml-auto flex items-center gap-3 text-xs font-semibold">
                <button
                  onClick={() => update.mutate({ id: d.id, required: !d.required })}
                  className={d.required ? "text-blueprint" : "text-graphite"}
                >
                  {d.required ? "Required" : "Optional"}
                </button>
                <button
                  onClick={() => {
                    if (window.confirm(`Delete “${d.label}”? Every value stored for it is removed.`))
                      del.mutate(d.id);
                  }}
                  className="text-graphite transition hover:text-critical"
                >
                  Delete
                </button>
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

/**
 * humanMinutes turns a budget into the unit somebody would have typed it in.
 * "2880 minutes" is unreadable; "2 days" is the thing that was actually agreed.
 */
function humanMinutes(m: number): string {
  if (m % (60 * 24) === 0) {
    const d = m / (60 * 24);
    return d === 1 ? "1 day" : `${d} days`;
  }
  if (m % 60 === 0) {
    const h = m / 60;
    return h === 1 ? "1 hour" : `${h} hours`;
  }
  return `${m} min`;
}

// Budgets are entered as a number plus a unit rather than raw minutes, because
// nobody knows what 2880 is and everybody knows what 2 days is.
const UNITS: { label: string; minutes: number }[] = [
  { label: "hours", minutes: 60 },
  { label: "days", minutes: 60 * 24 },
];

function SLASection({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const qc = useQueryClient();
  const policies = useQuery({
    queryKey: ["sla-policies", projectKey],
    queryFn: () => api.listSLAPolicies(projectKey),
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["sla-policies", projectKey] });

  const [severity, setSeverity] = useState("");
  const [type, setType] = useState("");
  const [respN, setRespN] = useState("4");
  const [respUnit, setRespUnit] = useState(60);
  const [resoN, setResoN] = useState("3");
  const [resoUnit, setResoUnit] = useState(60 * 24);

  const create = useMutation({
    mutationFn: () =>
      api.createSLAPolicy(projectKey, {
        severity,
        type,
        response_minutes: Math.round(Number(respN) * respUnit),
        resolution_minutes: Math.round(Number(resoN) * resoUnit),
      }),
    onSuccess: invalidate,
  });
  const update = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) =>
      api.updateSLAPolicy(id, { is_active }),
    onSuccess: invalidate,
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteSLAPolicy(id), onSuccess: invalidate });

  const items = policies.data?.items ?? [];

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">SLA policies</h2>
        <p className="text-sm leading-relaxed text-graphite">
          What this project commits to per severity: how long until somebody answers, and how long
          until it is fixed. A warning fires at 75% of a budget and a breach when it runs out — each
          once. Filter with{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">sla:breached</code>{" "}
          or{" "}
          <code className="rounded bg-panel px-1 py-0.5 font-mono text-xs text-ink">sla:at-risk</code>.
        </p>
        {/* The resolution order is the part people get wrong, and it is invisible in a
            flat list — an unqualified row that looks last can still be the one applied. */}
        <p className="text-sm leading-relaxed text-graphite-soft">
          The most specific matching policy wins; rows are listed in that order.
        </p>
      </div>

      {canManage && (
        <div className="flex flex-wrap items-end gap-3 rounded-md border border-hairline bg-panel/50 p-3">
          <label className="text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Severity
            </span>
            <select
              value={severity}
              onChange={(e) => setSeverity(e.target.value)}
              className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
            >
              <option value="">Any</option>
              {["critical", "high", "medium", "low"].map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Type
            </span>
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
            >
              <option value="">Any</option>
              {["bug", "task", "feature", "improvement"].map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <BudgetInput
            label="First response"
            n={respN}
            unit={respUnit}
            onN={setRespN}
            onUnit={setRespUnit}
          />
          <BudgetInput label="Resolution" n={resoN} unit={resoUnit} onN={setResoN} onUnit={setResoUnit} />
          <button
            disabled={create.isPending || !Number(respN) || !Number(resoN)}
            onClick={() => create.mutate()}
            className="flex h-9 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            <IconPlus size={15} />
            Add
          </button>
        </div>
      )}
      {create.isError && <p className="text-sm text-critical">{(create.error as Error).message}</p>}

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {policies.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {policies.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">
            No policies — issues in this project carry no SLA.
          </div>
        )}
        {items.map((p) => (
          <div key={p.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <span className="w-40 text-sm font-medium text-ink">
              {p.severity ?? "any severity"}
              {p.type ? ` · ${p.type}` : ""}
            </span>
            <span className="text-sm text-graphite">
              respond in <span className="font-semibold text-ink">{humanMinutes(p.response_minutes)}</span>
              {" · "}
              resolve in <span className="font-semibold text-ink">{humanMinutes(p.resolution_minutes)}</span>
            </span>
            {!p.is_active && (
              <span className="rounded-full border border-hairline bg-panel px-2 py-px text-xs text-graphite">
                paused
              </span>
            )}
            {canManage && (
              <span className="ml-auto flex items-center gap-3">
                <button
                  onClick={() => update.mutate({ id: p.id, is_active: !p.is_active })}
                  className="text-xs font-semibold text-blueprint transition hover:opacity-80"
                >
                  {p.is_active ? "Pause" : "Resume"}
                </button>
                <button
                  onClick={() => {
                    if (window.confirm("Delete this SLA policy? Issues it governs stop being measured."))
                      del.mutate(p.id);
                  }}
                  className="text-xs font-semibold text-graphite transition hover:text-critical"
                >
                  Delete
                </button>
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

function BudgetInput({
  label,
  n,
  unit,
  onN,
  onUnit,
}: {
  label: string;
  n: string;
  unit: number;
  onN: (v: string) => void;
  onUnit: (v: number) => void;
}) {
  return (
    <label className="text-sm">
      <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
        {label}
      </span>
      <span className="flex items-center gap-1">
        <input
          type="number"
          min={1}
          value={n}
          onChange={(e) => onN(e.target.value)}
          className="h-9 w-16 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
        />
        <select
          value={unit}
          onChange={(e) => onUnit(Number(e.target.value))}
          className="h-9 rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
        >
          {UNITS.map((u) => (
            <option key={u.label} value={u.minutes}>
              {u.label}
            </option>
          ))}
        </select>
      </span>
    </label>
  );
}

function MembersSection({ projectKey, canManage }: { projectKey: string; canManage: boolean }) {
  const qc = useQueryClient();
  const members = useQuery({
    queryKey: ["members", projectKey],
    queryFn: () => api.listProjectMembers(projectKey),
  });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const [newUser, setNewUser] = useState("");
  const [newRole, setNewRole] = useState("member");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["members", projectKey] });
  const put = useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: string }) =>
      api.putProjectMember(projectKey, userId, role),
    onSuccess: () => {
      setNewUser("");
      invalidate();
    },
  });
  const remove = useMutation({
    mutationFn: (userId: string) => api.removeProjectMember(projectKey, userId),
    onSuccess: invalidate,
  });

  const items = members.data?.items ?? [];
  const memberIds = new Set(items.map((m) => m.user.id));
  const candidates = (users.data?.items ?? []).filter((u) => !memberIds.has(u.id));

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-hairline bg-paper p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">Members</h2>
        <p className="text-sm leading-relaxed text-graphite">
          A project role <span className="text-ink">elevates</span> what a user can do here beyond their global role
          — e.g. a global reporter with project role maintainer can manage this project's components, milestones and
          releases. Global owners/admins always have full access.
        </p>
      </div>

      {canManage && (
        <div className="flex items-end gap-3">
          <label className="grow text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
              Add member
            </span>
            <select
              value={newUser}
              onChange={(e) => setNewUser(e.target.value)}
              className="w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint"
            >
              <option value="">Select a user…</option>
              {candidates.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.display_name || u.email}
                </option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Role</span>
            <select
              value={newRole}
              onChange={(e) => setNewRole(e.target.value)}
              className="rounded-md border border-hairline bg-paper px-2.5 py-2 text-sm capitalize text-ink outline-none focus:border-blueprint"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </label>
          <button
            disabled={!newUser || put.isPending}
            onClick={() => put.mutate({ userId: newUser, role: newRole })}
            className="flex h-10 items-center gap-1.5 rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            <IconPlus size={15} />
            Add
          </button>
        </div>
      )}
      {put.isError && <p className="text-sm text-critical">{(put.error as Error).message}</p>}

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {members.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {members.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">
            No project members yet — everyone works with their global role.
          </div>
        )}
        {items.map((m) => (
          <div key={m.user.id} className="flex items-center gap-3 p-3.5">
            <Avatar user={m.user as User} size={28} />
            <div className="min-w-0 grow">
              <div className="truncate text-sm font-medium text-ink">{m.user.display_name || m.user.email}</div>
              <div className="truncate text-xs text-graphite-soft">{m.user.email}</div>
            </div>
            <select
              value={m.role}
              disabled={!canManage || put.isPending}
              onChange={(e) => put.mutate({ userId: m.user.id, role: e.target.value })}
              aria-label="Project role"
              className="shrink-0 rounded-md border border-hairline bg-paper px-2.5 py-1.5 text-sm capitalize text-ink outline-none focus:border-blueprint disabled:opacity-60"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
            {canManage && (
              <button
                disabled={remove.isPending}
                onClick={() => {
                  if (window.confirm(`Remove ${m.user.display_name || m.user.email} from ${projectKey}?`))
                    remove.mutate(m.user.id);
                }}
                className="shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical disabled:opacity-50"
              >
                Remove
              </button>
            )}
          </div>
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
