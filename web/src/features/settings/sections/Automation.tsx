import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type AutomationRule } from "../../../lib/api";
import { timeAgo } from "../../../lib/activity";
import { Card, ErrorLine, dangerButtonClass, inputClass, quietButtonClass } from "../ui";

// Automation rules admin: when <event> [+ conditions] then <action>.
const RULE_EVENTS = ["issue.created", "comment.created", "issue.status_changed", "issue.resolved", "issue.reopened", "*"];
const ACTION_KINDS = [
  { kind: "set_priority", label: "set priority to", values: ["p0", "p1", "p2", "p3"] },
  { kind: "set_severity", label: "set severity to", values: ["critical", "high", "medium", "low"] },
  { kind: "add_label", label: "add label", values: null },
  { kind: "set_status", label: "transition to", values: ["in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened"] },
  { kind: "add_comment", label: "comment", values: null },
];

export function AutomationSection() {
  const qc = useQueryClient();
  const rules = useQuery({ queryKey: ["automation-rules"], queryFn: () => api.listAutomationRules() });
  const runs = useQuery({ queryKey: ["automation-runs"], queryFn: () => api.listAutomationRuns(), refetchInterval: 10000 });

  const [name, setName] = useState("");
  const [event, setEvent] = useState("issue.created");
  const [condSeverity, setCondSeverity] = useState("");
  const [actionKind, setActionKind] = useState("set_priority");
  const [actionValue, setActionValue] = useState("p1");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["automation-rules"] });
  const create = useMutation({
    mutationFn: () =>
      api.createAutomationRule({
        name: name.trim(),
        trigger: { event, conditions: condSeverity ? { severity: condSeverity } : undefined },
        actions: [{ kind: actionKind, value: actionValue.trim() }],
      }),
    onSuccess: () => {
      setName("");
      invalidate();
    },
  });
  const toggle = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) => api.updateAutomationRule(id, { is_active }),
    onSuccess: invalidate,
  });
  const edit = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Parameters<typeof api.updateAutomationRule>[1] }) =>
      api.updateAutomationRule(id, patch),
    onSuccess: () => {
      setEditingId(null);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteAutomationRule(id), onSuccess: invalidate });

  const [editingId, setEditingId] = useState<string | null>(null);
  const items = rules.data?.items ?? [];
  const kindMeta = ACTION_KINDS.find((k) => k.kind === actionKind)!;

  return (
    <Card
      title="Automation"
      description="When an event fires (optionally filtered), the Automation bot applies actions — e.g. new critical bugs get P0, or reopened issues get a triage label. Bot actions never re-trigger rules."
    >
      <div className="flex flex-wrap items-end gap-2">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Rule name" className={`${inputClass} w-40`} />
        <span className="pb-2 text-sm text-graphite-soft">when</span>
        <select value={event} onChange={(e) => setEvent(e.target.value)} className={inputClass}>
          {RULE_EVENTS.map((ev) => (
            <option key={ev} value={ev}>{ev === "*" ? "any event" : ev}</option>
          ))}
        </select>
        <select value={condSeverity} onChange={(e) => setCondSeverity(e.target.value)} className={inputClass} title="Optional severity condition">
          <option value="">any severity</option>
          {["critical", "high", "medium", "low"].map((s) => (
            <option key={s} value={s}>severity {s}</option>
          ))}
        </select>
        <span className="pb-2 text-sm text-graphite-soft">then</span>
        <select
          value={actionKind}
          onChange={(e) => {
            const meta = ACTION_KINDS.find((k) => k.kind === e.target.value)!;
            setActionKind(e.target.value);
            setActionValue(meta.values ? meta.values[0] : "");
          }}
          className={inputClass}
        >
          {ACTION_KINDS.map((k) => (
            <option key={k.kind} value={k.kind}>{k.label}</option>
          ))}
        </select>
        {kindMeta.values ? (
          <select value={actionValue} onChange={(e) => setActionValue(e.target.value)} className={inputClass}>
            {kindMeta.values.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
        ) : (
          <input value={actionValue} onChange={(e) => setActionValue(e.target.value)} placeholder="value…" className={`${inputClass} w-36`} />
        )}
        <button
          disabled={!name.trim() || !actionValue.trim() || create.isPending}
          onClick={() => create.mutate()}
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          Add rule
        </button>
      </div>
      <ErrorLine error={create.error} />

      <div className="flex flex-col divide-y divide-hairline overflow-hidden rounded-md border border-hairline">
        {rules.isSuccess && items.length === 0 && <div className="p-4 text-sm text-graphite-soft">No rules yet.</div>}
        {items.map((r) => (
          <div key={r.id} className="flex flex-col">
            <div className="flex items-center gap-3 p-3.5">
              <span className={`h-2 w-2 shrink-0 rounded-full ${r.is_active ? "bg-resolved" : "bg-hairline"}`} />
              <div className="min-w-0 grow">
                <div className="text-sm font-medium text-ink">{r.name}</div>
                <div className="truncate font-mono text-xs text-graphite-soft">
                  when {r.trigger.event}
                  {r.trigger.conditions && Object.entries(r.trigger.conditions).map(([k, v]) => ` · ${k}=${v}`)}
                  {" → "}
                  {r.actions.map((a) => `${a.kind}:${a.value}`).join(", ")}
                  {r.project_key ? ` · ${r.project_key}` : " · all projects"}
                </div>
              </div>
              <button onClick={() => setEditingId(editingId === r.id ? null : r.id)} className={quietButtonClass}>
                {editingId === r.id ? "Close" : "Edit"}
              </button>
              <button onClick={() => toggle.mutate({ id: r.id, is_active: !r.is_active })} className={quietButtonClass}>
                {r.is_active ? "Disable" : "Enable"}
              </button>
              <button
                onClick={() => {
                  if (window.confirm(`Delete rule “${r.name}”?`)) del.mutate(r.id);
                }}
                className={dangerButtonClass}
              >
                Delete
              </button>
            </div>
            {editingId === r.id && (
              <RuleEditor
                rule={r}
                pending={edit.isPending}
                error={edit.error as Error | null}
                onSave={(patch) => edit.mutate({ id: r.id, patch })}
                onCancel={() => setEditingId(null)}
              />
            )}
          </div>
        ))}
      </div>

      {(runs.data?.items.length ?? 0) > 0 && (
        <div className="flex flex-col gap-1">
          <span className="font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft">
            Recent runs
          </span>
          {runs.data!.items.slice(0, 8).map((run) => (
            <div key={run.id} className="flex items-center gap-3 font-mono text-xs">
              <span className={`w-14 font-semibold uppercase ${run.status === "matched" ? "text-resolved" : "text-critical"}`}>
                {run.status}
              </span>
              <span className="w-44 truncate text-graphite">{run.rule_name}</span>
              <span className="text-blueprint">{run.issue_key}</span>
              <span className="text-graphite-soft">{timeAgo(run.ran_at)}</span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

// RuleEditor edits a rule's actual logic — name, trigger event, severity condition
// and its action. Without it the UI could only toggle a rule on and off, so fixing a
// mistyped rule meant deleting and recreating it.
function RuleEditor({
  rule,
  pending,
  error,
  onSave,
  onCancel,
}: {
  rule: AutomationRule;
  pending: boolean;
  error: Error | null;
  onSave: (patch: Parameters<typeof api.updateAutomationRule>[1]) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(rule.name);
  const [event, setEvent] = useState(rule.trigger.event);
  const [severity, setSeverity] = useState(rule.trigger.conditions?.severity ?? "");
  const [kind, setKind] = useState(rule.actions[0]?.kind ?? "set_priority");
  const [value, setValue] = useState(rule.actions[0]?.value ?? "");

  const meta = ACTION_KINDS.find((k) => k.kind === kind) ?? ACTION_KINDS[0];

  return (
    <div className="flex flex-col gap-2 border-t border-hairline bg-panel/50 p-3.5">
      <div className="flex flex-wrap items-end gap-2">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Rule name" className={`${inputClass} w-40`} />
        <span className="pb-2 text-sm text-graphite-soft">when</span>
        <select value={event} onChange={(e) => setEvent(e.target.value)} className={inputClass}>
          {RULE_EVENTS.map((ev) => (
            <option key={ev} value={ev}>{ev === "*" ? "any event" : ev}</option>
          ))}
        </select>
        <select value={severity} onChange={(e) => setSeverity(e.target.value)} className={inputClass}>
          <option value="">any severity</option>
          {["critical", "high", "medium", "low"].map((sv) => (
            <option key={sv} value={sv}>severity {sv}</option>
          ))}
        </select>
        <span className="pb-2 text-sm text-graphite-soft">then</span>
        <select
          value={kind}
          onChange={(e) => {
            const m = ACTION_KINDS.find((k) => k.kind === e.target.value)!;
            setKind(e.target.value);
            setValue(m.values ? m.values[0] : "");
          }}
          className={inputClass}
        >
          {ACTION_KINDS.map((k) => (
            <option key={k.kind} value={k.kind}>{k.label}</option>
          ))}
        </select>
        {meta.values ? (
          <select value={value} onChange={(e) => setValue(e.target.value)} className={inputClass}>
            {meta.values.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
        ) : (
          <input value={value} onChange={(e) => setValue(e.target.value)} placeholder="value…" className={`${inputClass} w-36`} />
        )}
        <button
          disabled={!name.trim() || !value.trim() || pending}
          onClick={() =>
            onSave({
              name: name.trim(),
              trigger: { event, conditions: severity ? { severity } : undefined },
              actions: [{ kind, value: value.trim() }],
            })
          }
          className="h-[38px] rounded-md bg-blueprint px-4 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
        >
          {pending ? "Saving…" : "Save rule"}
        </button>
        <button onClick={onCancel} className="h-[38px] px-3 text-sm text-graphite transition hover:text-ink">
          Cancel
        </button>
      </div>
      <ErrorLine error={error} />
    </div>
  );
}
