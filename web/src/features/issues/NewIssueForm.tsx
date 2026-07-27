import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, type IssueType, type NewIssue, type Priority, type Severity } from "../../lib/api";
import { statusLabel, statusTone } from "../../components/Badges";
import { AssigneeSelect, ComponentsSelect, Field, LabelsInput, Modal, Select, TextInput, Textarea } from "./formFields";
import { NewIssueFields } from "./CustomFields";

const TYPES: IssueType[] = ["bug", "task", "feature", "improvement"];
const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

// Below this many characters a title is not yet a question worth asking the index.
const MIN_TITLE_FOR_LOOKUP = 8;
const LOOKUP_DEBOUNCE_MS = 400;

/**
 * SimilarIssues surfaces possible duplicates as the title is typed.
 *
 * Debounced and only past a few characters, so it costs one ranked query per pause
 * rather than one per keystroke. Suggestions never block filing — the point is to
 * inform, and a wrong guess must not stand between somebody and reporting a bug.
 */
function SimilarIssues({
  projectKey,
  title,
  duplicateOf,
  onMarkDuplicate,
}: {
  projectKey: string;
  title: string;
  duplicateOf: string | null;
  onMarkDuplicate: (key: string | null) => void;
}) {
  const [debounced, setDebounced] = useState("");

  useEffect(() => {
    const trimmed = title.trim();
    if (trimmed.length < MIN_TITLE_FOR_LOOKUP) {
      setDebounced("");
      return;
    }
    const t = setTimeout(() => setDebounced(trimmed), LOOKUP_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [title]);

  const similar = useQuery({
    queryKey: ["similar", projectKey, debounced],
    queryFn: () => api.similarIssues(projectKey, debounced),
    enabled: !!projectKey && debounced.length >= MIN_TITLE_FOR_LOOKUP,
    staleTime: 60_000,
  });

  const items = similar.data?.items ?? [];
  if (items.length === 0) return null;

  return (
    <div className="mt-2 rounded-md border border-hairline bg-panel/60 p-3">
      <p className="mb-2 font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
        Possibly already reported
      </p>
      <ul className="flex flex-col gap-1.5">
        {items.map((s) => (
          <li key={s.issue_key} className="flex items-center gap-2 text-sm">
            <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${statusTone[s.status].dot}`} title={statusLabel[s.status]} />
            <a
              href={`/issues/${s.issue_key}`}
              target="_blank"
              rel="noreferrer noopener"
              className="shrink-0 font-mono text-xs font-medium text-blueprint hover:underline"
            >
              {s.issue_key}
            </a>
            <span className="truncate text-xs text-graphite" title={s.title}>
              {s.title}
            </span>
            <span className="grow" />
            <button
              type="button"
              onClick={() => onMarkDuplicate(duplicateOf === s.issue_key ? null : s.issue_key)}
              className={`shrink-0 rounded-full border px-2 py-0.5 text-[11px] transition ${
                duplicateOf === s.issue_key
                  ? "border-blueprint bg-blueprint-soft font-medium text-blueprint"
                  : "border-hairline text-graphite-soft hover:border-graphite hover:text-graphite"
              }`}
            >
              {duplicateOf === s.issue_key ? "marked duplicate" : "duplicate of this"}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function NewIssueForm({ projectKey, onClose }: { projectKey: string; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState<NewIssue>({ type: "bug", title: "", priority: "p2", severity: "medium", labels: [] });

  const set = <K extends keyof NewIssue>(k: K, v: NewIssue[K]) => setForm((f) => ({ ...f, [k]: v }));

  // Marked before the issue exists; the relation is attached right after it is created.
  const [duplicateOf, setDuplicateOf] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: async () => {
      const body: NewIssue = { ...form };
      if (body.type !== "bug") delete body.severity;
      if (!body.assignee_id) delete body.assignee_id;
      // An empty string on create would be sent as "clear the due date" — harmless
      // but confusing in the request; drop it so an unset field is simply absent.
      if (!body.due_at) delete body.due_at;
      if (!body.estimate) delete body.estimate;
      if (body.fields && Object.keys(body.fields).length === 0) delete body.fields;
      const issue = await api.createIssue(projectKey, body);
      // Filing a duplicate on purpose is legitimate — it just has to be recorded, so
      // whoever triages it does not have to rediscover the connection.
      if (duplicateOf) {
        await api.addRelation(issue.key, "duplicates", duplicateOf).catch(() => undefined);
      }
      return issue;
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
          <AssigneeSelect value={form.assignee_id ?? ""} onChange={(v) => set("assignee_id", v)} />
        </Field>
        <Field label="Estimate (optional)">
          <TextInput
            value={form.estimate ?? ""}
            onChange={(v) => set("estimate", v)}
            placeholder="2d, 4h, 90m"
          />
        </Field>
        <Field label="Due date (optional)">
          {/* A date input, not a datetime one: people commit to a day. The API
              resolves a bare date to the end of it. */}
          <input
            type="date"
            value={form.due_at ?? ""}
            onChange={(e) => set("due_at", e.target.value)}
            className="h-10 w-full rounded-md border border-hairline bg-paper px-3 text-sm text-ink outline-none focus:border-blueprint"
          />
        </Field>
      </div>

      <Field label="Title" className="mt-3">
        <TextInput autoFocus value={form.title} onChange={(v) => set("title", v)} placeholder="Short summary" />
      </Field>
      <SimilarIssues
        projectKey={projectKey}
        title={form.title}
        duplicateOf={duplicateOf}
        onMarkDuplicate={setDuplicateOf}
      />
      <Field label="Description (Markdown)" className="mt-3">
        <Textarea value={form.description_md ?? ""} onChange={(v) => set("description_md", v)} rows={4} />
      </Field>
      <NewIssueFields
        projectKey={projectKey}
        issueType={form.type}
        values={form.fields ?? {}}
        onChange={(fields) => set("fields", fields)}
      />
      <Field label="Labels" className="mt-3">
        <LabelsInput projectKey={projectKey} value={form.labels ?? []} onChange={(v) => set("labels", v)} />
      </Field>
      <Field label="Components" className="mt-3">
        <ComponentsSelect projectKey={projectKey} value={form.components ?? []} onChange={(v) => set("components", v)} />
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
