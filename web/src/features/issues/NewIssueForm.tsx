import { useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, type IssueType, type NewIssue, type Priority, type Severity } from "../../lib/api";

const TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];
const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

export function NewIssueForm({ projectKey, onClose }: { projectKey: string; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState<NewIssue>({ type: "bug", title: "", priority: "p2", severity: "medium" });

  const set = <K extends keyof NewIssue>(k: K, v: NewIssue[K]) => setForm((f) => ({ ...f, [k]: v }));

  const create = useMutation({
    mutationFn: () => {
      const body: NewIssue = { ...form };
      if (body.type !== "bug") delete body.severity; // severity only applies to bugs
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
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-2xl border border-surface-border bg-surface-raised p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">New issue in {projectKey}</h2>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>

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
          <input
            autoFocus
            value={form.title}
            onChange={(e) => set("title", e.target.value)}
            placeholder="Short summary"
            className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 outline-none focus:border-accent"
          />
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

        {create.isError && (
          <p className="mt-3 text-sm text-severity-high">{(create.error as Error).message}</p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button onClick={onClose} className="rounded-lg px-4 py-2 text-sm text-slate-400 hover:text-slate-200">
            Cancel
          </button>
          <button
            disabled={!form.title.trim() || create.isPending}
            onClick={() => create.mutate()}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-50"
          >
            {create.isPending ? "Creating…" : "Create issue"}
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({ label, children, className = "" }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`block ${className}`}>
      <span className="mb-1 block text-xs uppercase tracking-wide text-slate-500">{label}</span>
      {children}
    </label>
  );
}

function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: string[] }) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 outline-none focus:border-accent"
    >
      {options.map((o) => (
        <option key={o} value={o}>{o}</option>
      ))}
    </select>
  );
}

function Textarea({ value, onChange, rows }: { value: string; onChange: (v: string) => void; rows: number }) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      rows={rows}
      className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 text-sm outline-none focus:border-accent"
    />
  );
}
