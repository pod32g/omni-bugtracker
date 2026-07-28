import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Label } from "../../../lib/api";
import { useProject } from "../../../lib/project";
import { Card, ErrorLine, dangerButtonClass, inputClass, quietButtonClass } from "../ui";

const DEFAULT_COLOR = "#8b5cf6";

/**
 * LabelsSection curates labels. They are created implicitly by typing them on an
 * issue, which is the right default and also how a project ends up with "regresion",
 * "regression" and "Regression" — three labels, one meaning, and no filter that finds
 * all of it. Renaming and merging are the repairs; the issue count is how you tell a
 * load-bearing label from a typo somebody made once.
 */
export function LabelsSection() {
  const qc = useQueryClient();
  const { projects, projectKey: currentKey } = useProject();
  const [scope, setScope] = useState("");
  const projectKey = scope || currentKey;

  const labels = useQuery({
    queryKey: ["labels", projectKey],
    queryFn: () => api.listLabels(projectKey),
    enabled: !!projectKey,
  });

  const [name, setName] = useState("");
  const [color, setColor] = useState(DEFAULT_COLOR);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [mergingId, setMergingId] = useState<string | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["labels"] });
  const create = useMutation({
    mutationFn: () => api.createLabel(projectKey, { name: name.trim(), color }),
    onSuccess: () => {
      setName("");
      setColor(DEFAULT_COLOR);
      invalidate();
    },
  });
  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Parameters<typeof api.updateLabel>[1] }) =>
      api.updateLabel(id, patch),
    onSuccess: () => {
      setEditingId(null);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteLabel(id), onSuccess: invalidate });
  const merge = useMutation({
    mutationFn: ({ id, intoId }: { id: string; intoId: string }) => api.mergeLabel(id, intoId),
    onSuccess: () => {
      setMergingId(null);
      invalidate();
    },
  });

  const items = labels.data?.items ?? [];

  return (
    <Card
      title="Labels"
      description="Labels appear the moment someone types one on an issue, so the list grows on its own. Rename to fix a spelling — every issue follows — or merge the duplicates into the one you meant."
      actions={
        projects.length > 1 && (
          <select
            value={projectKey}
            onChange={(e) => setScope(e.target.value)}
            aria-label="Project"
            className="h-9 rounded-md border border-hairline bg-paper px-2.5 text-sm text-ink outline-none focus:border-blueprint"
          >
            {projects.map((p) => (
              <option key={p.key} value={p.key}>
                {p.key}
              </option>
            ))}
          </select>
        )
      }
    >
      <div className="flex flex-wrap items-end gap-2">
        <input
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          aria-label="Label colour"
          title="Label colour"
          className="h-[38px] w-12 shrink-0 cursor-pointer rounded-md border border-hairline bg-paper p-1"
        />
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && name.trim()) create.mutate();
          }}
          maxLength={50}
          placeholder="New label name"
          className={`${inputClass} w-56`}
        />
        <button
          disabled={!name.trim() || !projectKey || create.isPending}
          onClick={() => create.mutate()}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          Add label
        </button>
      </div>
      <ErrorLine error={create.error} />
      <ErrorLine error={del.error} />
      <ErrorLine error={merge.error} />

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {labels.isLoading && <div className="p-4 text-sm text-graphite">Loading…</div>}
        {labels.isSuccess && items.length === 0 && (
          <div className="p-4 text-sm text-graphite-soft">No labels in {projectKey} yet.</div>
        )}
        {items.map((l) => (
          <div key={l.id} className="flex flex-col">
            {/* Wraps rather than shrinks: three fixed-width buttons against a
                min-w-0 name column crushed the name to one letter on a phone. */}
            <div className="flex flex-wrap items-center gap-3 p-3.5">
              <span
                className="h-3 w-3 shrink-0 rounded-full border border-hairline"
                style={{ backgroundColor: l.color }}
              />
              <div className="min-w-[10rem] grow basis-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-ink">{l.name}</span>
                  {l.is_global && (
                    <span className="shrink-0 rounded-sm bg-panel px-1 py-0.5 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                      global
                    </span>
                  )}
                </div>
                <div className="truncate font-mono text-xs text-graphite-soft">
                  {l.issue_count === 0 ? "unused" : `${l.issue_count} issue${l.issue_count === 1 ? "" : "s"}`}
                  {l.description ? ` · ${l.description}` : ""}
                </div>
              </div>
              <button
                onClick={() => {
                  setMergingId(null);
                  setEditingId(editingId === l.id ? null : l.id);
                }}
                className={quietButtonClass}
              >
                {editingId === l.id ? "Close" : "Edit"}
              </button>
              <button
                onClick={() => {
                  setEditingId(null);
                  setMergingId(mergingId === l.id ? null : l.id);
                }}
                className={quietButtonClass}
              >
                Merge
              </button>
              <button
                disabled={del.isPending}
                onClick={() => {
                  const carried =
                    l.issue_count > 0
                      ? ` ${l.issue_count} issue${l.issue_count === 1 ? "" : "s"} will lose it.`
                      : "";
                  if (window.confirm(`Delete “${l.name}”?${carried} The issues themselves are untouched.`))
                    del.mutate(l.id);
                }}
                className={dangerButtonClass}
              >
                Delete
              </button>
            </div>
            {editingId === l.id && (
              <LabelEditor
                label={l}
                pending={update.isPending}
                error={update.error as Error | null}
                onSave={(patch) => update.mutate({ id: l.id, patch })}
                onCancel={() => setEditingId(null)}
              />
            )}
            {mergingId === l.id && (
              <MergePicker
                label={l}
                candidates={items.filter((c) => c.id !== l.id && c.is_global === l.is_global)}
                pending={merge.isPending}
                onMerge={(intoId) => merge.mutate({ id: l.id, intoId })}
                onCancel={() => setMergingId(null)}
              />
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}

function LabelEditor({
  label,
  pending,
  error,
  onSave,
  onCancel,
}: {
  label: Label;
  pending: boolean;
  error: Error | null;
  onSave: (patch: { name: string; color: string; description: string }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(label.name);
  const [color, setColor] = useState(label.color || DEFAULT_COLOR);
  const [description, setDescription] = useState(label.description ?? "");

  return (
    <div className="flex flex-col gap-2 border-t border-hairline bg-panel/50 p-3.5">
      <div className="flex flex-wrap items-end gap-2">
        <input
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          aria-label="Label colour"
          className="h-[38px] w-12 shrink-0 cursor-pointer rounded-md border border-hairline bg-paper p-1"
        />
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={50}
          placeholder="Name"
          className={`${inputClass} w-48`}
        />
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What it means (optional)"
          className={`${inputClass} w-64`}
        />
        <button
          disabled={!name.trim() || pending}
          onClick={() => onSave({ name: name.trim(), color, description: description.trim() })}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {pending ? "Saving…" : "Save label"}
        </button>
        <button onClick={onCancel} className="h-[38px] px-3 text-sm text-graphite transition hover:text-ink">
          Cancel
        </button>
      </div>
      <ErrorLine error={error} />
    </div>
  );
}

// Merging is one-way and deletes the label you started from, so the confirm spells
// out which name survives rather than asking "are you sure?".
function MergePicker({
  label,
  candidates,
  pending,
  onMerge,
  onCancel,
}: {
  label: Label;
  candidates: Label[];
  pending: boolean;
  onMerge: (intoId: string) => void;
  onCancel: () => void;
}) {
  const [target, setTarget] = useState(candidates[0]?.id ?? "");

  return (
    <div className="flex flex-wrap items-center gap-2 border-t border-hairline bg-panel/50 p-3.5 text-sm">
      {candidates.length === 0 ? (
        <span className="text-graphite-soft">
          Nothing to merge into — “{label.name}” is the only {label.is_global ? "global " : ""}label here.
        </span>
      ) : (
        <>
          <span className="text-graphite">
            Move every issue tagged <span className="font-medium text-ink">{label.name}</span> to
          </span>
          <select
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            aria-label="Merge into"
            className={inputClass}
          >
            {candidates.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
          <button
            disabled={!target || pending}
            onClick={() => {
              const into = candidates.find((c) => c.id === target);
              if (
                into &&
                window.confirm(
                  `Merge “${label.name}” into “${into.name}”? ${label.issue_count} issue${
                    label.issue_count === 1 ? "" : "s"
                  } move across and “${label.name}” is deleted.`,
                )
              )
                onMerge(target);
            }}
            className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
          >
            {pending ? "Merging…" : "Merge"}
          </button>
        </>
      )}
      <button onClick={onCancel} className="h-[38px] px-3 text-sm text-graphite transition hover:text-ink">
        Cancel
      </button>
    </div>
  );
}
