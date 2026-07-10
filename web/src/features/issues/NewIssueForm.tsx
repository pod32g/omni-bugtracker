import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, type IssueType, type NewIssue, type Priority, type Severity } from "../../lib/api";
import { AssigneeSelect, ComponentsSelect, Field, LabelsInput, Modal, Select, TextInput, Textarea } from "./formFields";

const TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];
const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

export function NewIssueForm({ projectKey, onClose }: { projectKey: string; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState<NewIssue>({ type: "bug", title: "", priority: "p2", severity: "medium", labels: [] });

  const set = <K extends keyof NewIssue>(k: K, v: NewIssue[K]) => setForm((f) => ({ ...f, [k]: v }));

  const create = useMutation({
    mutationFn: () => {
      const body: NewIssue = { ...form };
      if (body.type !== "bug") delete body.severity;
      if (!body.assignee_id) delete body.assignee_id;
      return api.createIssue(projectKey, body);
    },
    onSuccess: (issue) => {
      qc.invalidateQueries({ queryKey: ["issues"] });
      onClose();
      navigate(`/issues/${issue.key}`);
    },
  });

  const isBug = form.type === "bug";

  return (
    <Modal title={`New issue in ${projectKey}`} onClose={onClose}>
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
        <Field label="Assignee">
          <AssigneeSelect value={form.assignee_id ?? ""} onChange={(v) => set("assignee_id", v)} />
        </Field>
      </div>

      <Field label="Title" className="mt-3">
        <TextInput autoFocus value={form.title} onChange={(v) => set("title", v)} placeholder="Short summary" />
      </Field>
      <Field label="Description (Markdown)" className="mt-3">
        <Textarea value={form.description_md ?? ""} onChange={(v) => set("description_md", v)} rows={4} />
      </Field>
      <Field label="Labels" className="mt-3">
        <LabelsInput projectKey={projectKey} value={form.labels ?? []} onChange={(v) => set("labels", v)} />
      </Field>
      <Field label="Components" className="mt-3">
        <ComponentsSelect projectKey={projectKey} value={form.components ?? []} onChange={(v) => set("components", v)} />
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

      {create.isError && <p className="mt-3 text-sm text-critical">{(create.error as Error).message}</p>}

      <div className="mt-5 flex justify-end gap-2">
        <button onClick={onClose} className="rounded-md px-4 py-2 text-sm text-graphite hover:text-ink">Cancel</button>
        <button
          disabled={!form.title.trim() || create.isPending}
          onClick={() => create.mutate()}
          className="rounded-md bg-blueprint px-4 py-2 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {create.isPending ? "Creating…" : "Create issue"}
        </button>
      </div>
    </Modal>
  );
}
