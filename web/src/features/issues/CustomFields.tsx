import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type FieldDefinition, type FieldValue, type User } from "../../lib/api";

/**
 * Custom fields are rendered generically, from the definition's type alone. Nothing
 * here knows a project's field keys, and nothing ever should — the moment one
 * component special-cases `customer_tier`, the feature stops being project-scoped
 * structure and becomes a hardcoded column with extra steps.
 */

/** FieldInput is the single control per type, shared by the create form and the rail. */
export function FieldInput({
  def,
  value,
  users,
  onChange,
}: {
  def: FieldDefinition;
  value: unknown;
  users: User[];
  onChange: (v: unknown) => void;
}) {
  const base =
    "h-[30px] w-full rounded-md border border-hairline bg-paper px-2 text-sm text-ink outline-none focus:border-blueprint";

  switch (def.type) {
    case "select":
      return (
        <select value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value)} className={base}>
          <option value="">—</option>
          {def.options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      );

    case "multi_select": {
      const selected = Array.isArray(value) ? (value as string[]) : [];
      return (
        <div className="flex flex-wrap gap-1.5">
          {def.options.map((o) => {
            const on = selected.includes(o);
            return (
              <button
                key={o}
                type="button"
                onClick={() => onChange(on ? selected.filter((s) => s !== o) : [...selected, o])}
                className={`rounded-full border px-2.5 py-0.5 text-xs font-medium transition ${
                  on
                    ? "border-blueprint bg-blueprint-soft text-blueprint"
                    : "border-hairline text-graphite hover:border-graphite hover:text-ink"
                }`}
              >
                {o}
              </button>
            );
          })}
        </div>
      );
    }

    case "checkbox":
      return (
        <label className="flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
            className="accent-blueprint"
          />
          {def.label}
        </label>
      );

    case "number":
      return (
        <input
          type="number"
          value={value == null ? "" : String(value)}
          // An empty box means "unset", not zero — the API distinguishes the two, so
          // the control has to as well.
          onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
          className={base}
        />
      );

    case "date":
      return (
        <input type="date" value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value)} className={base} />
      );

    case "user":
      return (
        <select value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value)} className={base}>
          <option value="">Unassigned</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>
              {u.display_name || u.email}
            </option>
          ))}
        </select>
      );

    case "url":
      return (
        <input
          type="url"
          value={(value as string) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          placeholder="https://…"
          className={base}
        />
      );

    default:
      return (
        <input
          value={(value as string) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          className={base}
        />
      );
  }
}

/** Read-only rendering, for the detail rail before anybody clicks edit. */
export function FieldValueText({ field }: { field: FieldValue }) {
  const v = field.value;
  if (v == null || v === "" || (Array.isArray(v) && v.length === 0)) {
    return <span className="text-sm text-graphite-soft">—</span>;
  }
  if (field.type === "checkbox") {
    return <span className="text-sm text-ink">{v ? "Yes" : "No"}</span>;
  }
  if (field.type === "user") {
    return <span className="text-sm text-ink">{field.user?.display_name || field.user?.email || "—"}</span>;
  }
  if (field.type === "url") {
    return (
      <a href={String(v)} target="_blank" rel="noreferrer" className="text-sm text-blueprint hover:underline">
        {String(v)}
      </a>
    );
  }
  if (Array.isArray(v)) {
    return (
      <span className="flex flex-wrap gap-1">
        {v.map((item) => (
          <span key={item} className="rounded-full border border-hairline bg-panel px-2 py-px text-xs text-graphite">
            {item}
          </span>
        ))}
      </span>
    );
  }
  return <span className="text-sm text-ink">{String(v)}</span>;
}

/**
 * CustomFieldsPanel is the detail rail's block: read the values, edit them in place.
 *
 * Renders nothing when the project defines no fields — an empty "Fields" heading on
 * every issue in every project that has never used them is pure noise.
 */
export function CustomFieldsPanel({ issueKey, projectKey }: { issueKey: string; projectKey: string }) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, unknown>>({});

  const defs = useQuery({
    queryKey: ["field-defs", projectKey],
    queryFn: () => api.listFieldDefinitions(projectKey),
    enabled: !!projectKey,
  });
  const values = useQuery({
    queryKey: ["issue-fields", issueKey],
    queryFn: () => api.listIssueFields(issueKey),
  });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });

  const items = values.data?.items ?? [];
  // Seed the draft from the server's values whenever they change, so opening the editor
  // never starts from a stale copy of what somebody else already changed.
  useEffect(() => {
    setDraft(Object.fromEntries(items.map((f) => [f.key, f.value])));
  }, [values.data]);

  const save = useMutation({
    mutationFn: () => api.setIssueFields(issueKey, draft),
    onSuccess: () => {
      setEditing(false);
      qc.invalidateQueries({ queryKey: ["issue-fields", issueKey] });
      qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });

  if (items.length === 0) return null;
  const byKey = new Map((defs.data?.items ?? []).map((d) => [d.key, d]));

  return (
    <div className="flex flex-col gap-3 border-t border-hairline pt-5">
      <div className="flex items-center justify-between">
        <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">Fields</span>
        <button
          onClick={() => setEditing((e) => !e)}
          className="text-xs font-semibold text-blueprint transition hover:opacity-80"
        >
          {editing ? "Cancel" : "Edit"}
        </button>
      </div>

      {items.map((f) => {
        const def = byKey.get(f.key);
        return (
          <div key={f.key} className="flex flex-col gap-1">
            <span className="text-xs text-graphite">
              {f.label}
              {def?.required && <span className="text-critical"> *</span>}
            </span>
            {editing && def ? (
              <FieldInput
                def={def}
                value={draft[f.key]}
                users={users.data?.items ?? []}
                onChange={(v) => setDraft((d) => ({ ...d, [f.key]: v }))}
              />
            ) : (
              <FieldValueText field={f} />
            )}
            {editing && def?.help_text && (
              <span className="text-xs text-graphite-soft">{def.help_text}</span>
            )}
          </div>
        );
      })}

      {editing && (
        <div className="flex items-center gap-3">
          <button
            disabled={save.isPending}
            onClick={() => save.mutate()}
            className="rounded-md bg-blueprint px-3 py-1 text-xs font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            {save.isPending ? "Saving…" : "Save fields"}
          </button>
          {save.isError && <span className="text-xs text-critical">{(save.error as Error).message}</span>}
        </div>
      )}
    </div>
  );
}

/** The create form's block: the same controls, before the issue exists. */
export function NewIssueFields({
  projectKey,
  issueType,
  values,
  onChange,
}: {
  projectKey: string;
  issueType: string;
  values: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
}) {
  const defs = useQuery({
    queryKey: ["field-defs", projectKey],
    queryFn: () => api.listFieldDefinitions(projectKey),
    enabled: !!projectKey,
  });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });

  // An empty applies_to means every type; otherwise the field only shows for the
  // types it was declared for, so switching the type re-renders the form.
  const applicable = (defs.data?.items ?? []).filter(
    (d) => d.applies_to.length === 0 || d.applies_to.some((t) => t === issueType),
  );
  if (applicable.length === 0) return null;

  return (
    <div className="mt-3 flex flex-col gap-3 border-t border-hairline pt-3">
      {applicable.map((def) => (
        <label key={def.id} className="flex flex-col gap-1 text-sm">
          <span className="font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
            {def.label}
            {def.required && <span className="text-critical"> *</span>}
          </span>
          <FieldInput
            def={def}
            value={values[def.key]}
            users={users.data?.items ?? []}
            onChange={(v) => onChange({ ...values, [def.key]: v })}
          />
          {def.help_text && <span className="text-xs text-graphite-soft">{def.help_text}</span>}
        </label>
      ))}
    </div>
  );
}
