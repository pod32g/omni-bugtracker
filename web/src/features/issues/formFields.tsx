import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";

export function Field({ label, children, className = "" }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`block ${className}`}>
      <span className="mb-1 block text-xs uppercase tracking-wide text-slate-500">{label}</span>
      {children}
    </label>
  );
}

export function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: string[] }) {
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

export function Textarea({ value, onChange, rows }: { value: string; onChange: (v: string) => void; rows: number }) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      rows={rows}
      className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 text-sm outline-none focus:border-accent"
    />
  );
}

export function AssigneeSelect({
  value,
  onChange,
  unassignedValue = "",
}: {
  value: string;
  onChange: (v: string) => void;
  unassignedValue?: string;
}) {
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 outline-none focus:border-accent"
    >
      <option value={unassignedValue}>Unassigned</option>
      {users.data?.items.map((u) => (
        <option key={u.id} value={u.id}>{u.display_name || u.email}</option>
      ))}
    </select>
  );
}

export function LabelsInput({
  projectKey,
  value,
  onChange,
}: {
  projectKey: string;
  value: string[];
  onChange: (v: string[]) => void;
}) {
  const labels = useQuery({
    queryKey: ["labels", projectKey],
    queryFn: () => api.listLabels(projectKey),
    enabled: !!projectKey,
  });
  const [text, setText] = useState("");
  const add = (name: string) => {
    const n = name.trim();
    if (n && !value.includes(n)) onChange([...value, n]);
    setText("");
  };
  const suggestions = (labels.data?.items ?? []).map((l) => l.name).filter((n) => !value.includes(n));

  return (
    <div>
      <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-surface-border bg-surface px-2 py-1.5">
        {value.map((l) => (
          <span key={l} className="flex items-center gap-1 rounded-full bg-accent/20 px-2 py-0.5 text-xs text-accent-hover">
            {l}
            <button type="button" onClick={() => onChange(value.filter((v) => v !== l))} className="hover:text-white">×</button>
          </span>
        ))}
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              add(text);
            } else if (e.key === "Backspace" && !text && value.length) {
              onChange(value.slice(0, -1));
            }
          }}
          placeholder="Add label…"
          className="min-w-[6rem] flex-1 bg-transparent text-sm outline-none"
        />
      </div>
      {suggestions.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {suggestions.slice(0, 10).map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => add(s)}
              className="rounded-full border border-surface-border px-2 py-0.5 text-xs text-slate-400 hover:border-accent hover:text-accent-hover"
            >
              + {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function TextInput({ value, onChange, placeholder, autoFocus }: { value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean }) {
  return (
    <input
      autoFocus={autoFocus}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-lg border border-surface-border bg-surface px-3 py-2 outline-none focus:border-accent"
    />
  );
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-2xl border border-surface-border bg-surface-raised p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        {children}
      </div>
    </div>
  );
}
