import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";

const inputClass =
  "w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint";

export function Field({ label, children, className = "" }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`block ${className}`}>
      <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">{label}</span>
      {children}
    </label>
  );
}

export function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: string[] }) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} className={inputClass}>
      {options.map((o) => (
        <option key={o} value={o}>
          {o}
        </option>
      ))}
    </select>
  );
}

export function Textarea({ value, onChange, rows }: { value: string; onChange: (v: string) => void; rows: number }) {
  return <textarea value={value} onChange={(e) => onChange(e.target.value)} rows={rows} className={inputClass} />;
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
    <select value={value} onChange={(e) => onChange(e.target.value)} className={inputClass}>
      <option value={unassignedValue}>Unassigned</option>
      {users.data?.items.map((u) => (
        <option key={u.id} value={u.id}>
          {u.display_name || u.email}
        </option>
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
      <div className="flex flex-wrap items-center gap-1.5 rounded-md border border-hairline bg-paper px-2 py-1.5">
        {value.map((l) => (
          <span
            key={l}
            className="flex items-center gap-1 rounded-full bg-blueprint-soft px-2 py-0.5 text-xs font-medium text-blueprint"
          >
            {l}
            <button type="button" onClick={() => onChange(value.filter((v) => v !== l))} className="hover:text-ink">
              ×
            </button>
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
          className="min-w-[6rem] flex-1 bg-transparent text-sm text-ink outline-none placeholder:text-graphite-soft"
        />
      </div>
      {suggestions.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {suggestions.slice(0, 10).map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => add(s)}
              className="rounded-full border border-hairline px-2 py-0.5 text-xs text-graphite transition hover:border-blueprint hover:text-blueprint"
            >
              + {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ComponentsSelect renders the project's components as toggleable chips. Components are
// managed structure (created in project settings), so there's no free-text entry here.
export function ComponentsSelect({
  projectKey,
  value,
  onChange,
}: {
  projectKey: string;
  value: string[];
  onChange: (v: string[]) => void;
}) {
  const components = useQuery({
    queryKey: ["components", projectKey],
    queryFn: () => api.listComponents(projectKey),
    enabled: !!projectKey,
  });
  const items = components.data?.items ?? [];
  if (components.isSuccess && items.length === 0) {
    return <p className="text-xs text-graphite-soft">No components defined — add them in project settings.</p>;
  }
  const toggle = (name: string) =>
    onChange(value.includes(name) ? value.filter((v) => v !== name) : [...value, name]);
  return (
    <div className="flex flex-wrap gap-1.5">
      {items.map((c) => {
        const active = value.includes(c.name);
        return (
          <button
            key={c.id}
            type="button"
            onClick={() => toggle(c.name)}
            className={`rounded-full border px-2.5 py-1 text-xs font-medium transition ${
              active
                ? "border-blueprint bg-blueprint-soft text-blueprint"
                : "border-hairline text-graphite hover:border-blueprint hover:text-blueprint"
            }`}
          >
            {c.name}
          </button>
        );
      })}
    </div>
  );
}

export function TextInput({
  value,
  onChange,
  placeholder,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  return (
    <input
      autoFocus={autoFocus}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className={`${inputClass} placeholder:text-graphite-soft`}
    />
  );
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-ink/40 p-4" onClick={onClose}>
      <div
        className="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-lg border border-hairline bg-paper p-6 shadow-xl shadow-ink/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-bold text-ink">{title}</h2>
          <button onClick={onClose} className="text-graphite transition hover:text-ink">
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
