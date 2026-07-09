import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Issue, type IssueType, type NewIssue, type Priority, type Severity } from "../../lib/api";
import { Field, Modal, Select, TextInput, Textarea } from "./formFields";

const TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];
const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

export function EditIssueForm({ issue, onClose }: { issue: Issue; onClose: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<NewIssue>({
    type: issue.type,
    title: issue.title,
    priority: issue.priority,
    severity: issue.severity ?? "medium",
    description_md: issue.description_md ?? "",
    repro_steps_md: issue.repro_steps_md ?? "",
    expected_md: issue.expected_md ?? "",
    actual_md: issue.actual_md ?? "",
    environment_md: issue.environment_md ?? "",
  });

  const set = <K extends keyof NewIssue>(k: K, v: NewIssue[K]) => setForm((f) => ({ ...f, [k]: v }));

  const save = useMutation({
    mutationFn: () => {
      const patch: Partial<NewIssue> = { ...form };
      if (patch.type !== "bug") delete patch.severity;
      return api.updateIssue(issue.key, patch);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issues"] });
      qc.invalidateQueries({ queryKey: ["activity", issue.key] });
      onClose();
    },
  });

  const isBug = form.type === "bug";

  return (
    <Modal title={`Edit ${issue.key}`} onClose={onClose}>
      <div className="grid grid-cols-2 gap-3">
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
      </div>

      <Field label="Title" className="mt-3">
        <TextInput autoFocus value={form.title} onChange={(v) => set("title", v)} placeholder="Short summary" />
      </Field>
      <Field label="Description (Markdown)" className="mt-3">
        <Textarea value={form.description_md ?? ""} onChange={(v) => set("description_md", v)} rows={4} />
      </Field>

      {isBug && (
        <div className="mt-3 grid grid-cols-2 gap-3">
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

      {save.isError && <p className="mt-3 text-sm text-severity-high">{(save.error as Error).message}</p>}

      <div className="mt-5 flex justify-end gap-2">
        <button onClick={onClose} className="rounded-lg px-4 py-2 text-sm text-slate-400 hover:text-slate-200">Cancel</button>
        <button
          disabled={!form.title.trim() || save.isPending}
          onClick={() => save.mutate()}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-50"
        >
          {save.isPending ? "Saving…" : "Save changes"}
        </button>
      </div>
    </Modal>
  );
}
