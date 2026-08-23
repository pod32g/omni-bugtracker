import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  api,
  ApiError,
  UNASSIGNED,
  type Issue,
  type IssueType,
  type NewIssue,
  type Priority,
  type Severity,
} from "../../lib/api";
import { issueKeys } from "../../lib/queryKeys";
import { AssigneeSelect, ComponentsSelect, Field, LabelsInput, Modal, Select, TextInput, Textarea } from "./formFields";

const TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];
const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

/**
 * The editable fields of an issue, in the shape the form holds them. Used both to
 * seed the form and — re-read at submit time — as the baseline the form is diffed
 * against, so the two can never drift apart.
 */
const snapshotOf = (issue: Issue): NewIssue => ({
  type: issue.type,
  title: issue.title,
  priority: issue.priority,
  severity: issue.severity ?? "medium",
  assignee_id: issue.assignee?.id ?? UNASSIGNED,
  labels: issue.labels ?? [],
  components: issue.components ?? [],
  description_md: issue.description_md ?? "",
  repro_steps_md: issue.repro_steps_md ?? "",
  expected_md: issue.expected_md ?? "",
  actual_md: issue.actual_md ?? "",
  environment_md: issue.environment_md ?? "",
});

/** Order-insensitive for the two array fields; plain equality for the rest. */
function sameValue(a: unknown, b: unknown): boolean {
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    const sortedA = [...a].sort();
    const sortedB = [...b].sort();
    return sortedA.every((v, i) => v === sortedB[i]);
  }
  return a === b;
}

export function EditIssueForm({ issue, onClose }: { issue: Issue; onClose: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<NewIssue>(() => snapshotOf(issue));

  const set = <K extends keyof NewIssue>(k: K, v: NewIssue[K]) => setForm((f) => ({ ...f, [k]: v }));

  const save = useMutation({
    mutationFn: () => {
      // Send only what this user actually changed.
      //
      // `form` is a snapshot taken when the modal mounted, so PATCHing all eleven
      // fields writes back stale values for the ten they never touched: fixing a typo
      // in the title also reverted a colleague's rewritten description, with nothing
      // on screen to suggest it had happened. Diffing against the current `issue`
      // narrows the blast radius to the fields in dispute — two people editing the
      // *same* field still race, which is what a version precondition is for.
      const patch: Partial<NewIssue> = {};
      const current = snapshotOf(issue);
      for (const k of Object.keys(form) as (keyof NewIssue)[]) {
        if (!sameValue(form[k], current[k])) patch[k] = form[k] as never;
      }
      if (form.type !== "bug") delete patch.severity;
      return api.updateIssue(issue.key, patch);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: issueKeys.all });
      qc.invalidateQueries({ queryKey: ["activity", issue.key] });
      onClose();
    },
  });

  const isBug = form.type === "bug";
  const fieldErrors = save.error instanceof ApiError ? save.error.errors : {};

  return (
    <Modal title={`Edit ${issue.key}`} onClose={onClose}>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Type">
          <Select value={form.type} onChange={(v) => set("type", v as IssueType)} options={TYPES} />
        </Field>
        <Field label="Priority">
          <Select value={form.priority} onChange={(v) => set("priority", v as Priority)} options={PRIORITIES} />
        </Field>
        {isBug && (
          <Field label="Severity">
            <Select value={form.severity ?? "medium"} onChange={(v) => set("severity", v as Severity)} options={SEVERITIES} />
          </Field>
        )}
        <Field label="Assignee">
          <AssigneeSelect
            value={form.assignee_id ?? UNASSIGNED}
            onChange={(v) => set("assignee_id", v)}
            unassignedValue={UNASSIGNED}
          />
        </Field>
      </div>

      <Field label="Title" className="mt-3" error={fieldErrors.title}>
        <TextInput autoFocus value={form.title} onChange={(v) => set("title", v)} placeholder="Short summary" />
      </Field>
      <Field label="Description (Markdown)" className="mt-3" error={fieldErrors.description_md}>
        <Textarea value={form.description_md ?? ""} onChange={(v) => set("description_md", v)} rows={4} />
      </Field>
      <Field label="Labels" className="mt-3">
        <LabelsInput projectKey={issue.project_key} value={form.labels ?? []} onChange={(v) => set("labels", v)} />
      </Field>
      <Field label="Components" className="mt-3">
        <ComponentsSelect
          projectKey={issue.project_key}
          value={form.components ?? []}
          onChange={(v) => set("components", v)}
        />
      </Field>

      {isBug && (
        <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Reproduction steps">
            <Textarea value={form.repro_steps_md ?? ""} onChange={(v) => set("repro_steps_md", v)} rows={3} />
          </Field>
          <Field label="Environment">
            <Textarea value={form.environment_md ?? ""} onChange={(v) => set("environment_md", v)} rows={3} />
          </Field>
          <Field label="Expected behavior">
            <Textarea value={form.expected_md ?? ""} onChange={(v) => set("expected_md", v)} rows={2} />
          </Field>
          <Field label="Actual behavior">
            <Textarea value={form.actual_md ?? ""} onChange={(v) => set("actual_md", v)} rows={2} />
          </Field>
        </div>
      )}

      {save.isError && <p className="mt-3 text-sm text-critical">{(save.error as Error).message}</p>}

      <div className="mt-5 flex justify-end gap-2">
        <button onClick={onClose} className="rounded-md px-4 py-2 text-sm text-graphite hover:text-ink">Cancel</button>
        <button
          disabled={!form.title.trim() || save.isPending}
          onClick={() => save.mutate()}
          className="rounded-md bg-blueprint px-4 py-2 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {save.isPending ? "Saving…" : "Save changes"}
        </button>
      </div>
    </Modal>
  );
}
