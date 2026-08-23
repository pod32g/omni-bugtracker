import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";

const inputClass =
  "w-full rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint";

/**
 * `error` is the server's message for this specific field, from an RFC 9457 `errors`
 * member. Rendering it here rather than only in the form-level banner is what makes a
 * rejection actionable: the server names the field it refused, and the user should not
 * have to work out which of eleven inputs that was.
 */
export function Field({
  label,
  children,
  className = "",
  error,
}: {
  label: string;
  children: ReactNode;
  className?: string;
  error?: string;
}) {
  return (
    <label className={`block ${className}`}>
      <span className="mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft">{label}</span>
      {children}
      {error && (
        <span role="alert" className="mt-1 block text-xs text-critical">
          {error}
        </span>
      )}
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
      // React calls .focus() for autoFocus but emits no attribute, so a dialog cannot
      // find "the field that wanted focus" by selector. This publishes that intent in
      // the DOM — see Modal, which prefers it over the first field in document order
      // (which here is the estimate box, three fields above the one anybody wants).
      data-autofocus={autoFocus ? "" : undefined}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className={`${inputClass} placeholder:text-graphite-soft`}
    />
  );
}

/**
 * A modal dialog.
 *
 * `confirmClose` guards the dismissal paths that are easy to hit by accident. The new
 * issue form is the reason: a stray click on the backdrop threw away a bug report
 * somebody had spent five minutes writing, with no warning and no way back. Escape and
 * the backdrop ask first when it returns true; the explicit Cancel button never does,
 * because that one is a decision rather than a slip.
 *
 * Focus is trapped and restored. Without it, Tab walks straight out of the dialog into
 * the page behind — which for a screen-reader or keyboard user means the dialog is a
 * visual convention they cannot perceive the edges of.
 */
export function Modal({
  title,
  onClose,
  children,
  confirmClose,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  confirmClose?: () => boolean;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const headingID = useId();
  // Captured during render, not in the effect below.
  //
  // React applies autoFocus while attaching the dialog's DOM, which happens *before*
  // effects run — so reading document.activeElement in the effect returns the title
  // input inside the dialog, and "restoring" focus on close aimed at a node that was
  // about to be unmounted. Focus then fell to the body. Render runs before any of the
  // children exist, so this sees the element that actually opened the dialog.
  const openerRef = useRef<HTMLElement | null>(null);
  if (openerRef.current === null) {
    openerRef.current = document.activeElement as HTMLElement | null;
  }

  const requestClose = useCallback(() => {
    if (confirmClose?.() && !window.confirm("Discard your changes?")) return;
    onClose();
  }, [confirmClose, onClose]);

  useEffect(() => {
    // Move focus into the dialog so the next Tab lands inside it — unless something in
    // there already claimed it. A field marked autoFocus (the title, on the new issue
    // form) has by this point already been focused, and stealing it back to the close
    // button would put the caret nowhere useful.
    const panel = panelRef.current;
    if (panel && !panel.contains(document.activeElement)) {
      // The field the form nominated, else the first form control — not simply the
      // first focusable, which is the close button, and putting the caret there means
      // a keyboard user tabs past the whole form to reach what they came to fill in.
      //
      // Chosen explicitly rather than leaning on autoFocus: React applies that during
      // commit, and whether it has happened by the time this effect runs differs
      // between a fresh page load and a dialog opened from a click. This behaves the
      // same either way.
      const field =
        panel.querySelector<HTMLElement>("[data-autofocus]") ??
        panel.querySelector<HTMLElement>(FIRST_FIELD);
      (field ?? panel.querySelector<HTMLElement>(FOCUSABLE) ?? panel).focus();
    }

    // The page behind must not scroll under the dialog.
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    return () => {
      document.body.style.overflow = prevOverflow;
      openerRef.current?.focus?.();
    };
  }, []);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      requestClose();
      return;
    }
    if (e.key !== "Tab") return;
    // Cycle within the dialog rather than escaping to the page behind it.
    const items = Array.from(panelRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
      .filter((el) => el.offsetParent !== null || el === document.activeElement);
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    // Below sm the dialog fills the screen: a centred card with 16px of gutter wastes
    // most of a phone and leaves the form scrolling inside a box inside a page.
    <div
      className="fixed inset-0 z-50 grid place-items-end bg-ink/40 sm:place-items-center sm:p-4"
      onMouseDown={(e) => {
        // mousedown on the backdrop *itself*, not a drag that began inside the panel
        // and happened to end out here — releasing a text selection outside the dialog
        // used to close it.
        if (e.target === e.currentTarget) requestClose();
      }}
      onKeyDown={onKeyDown}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingID}
        tabIndex={-1}
        className="max-h-[92vh] w-full overflow-auto border-hairline bg-paper p-4 shadow-xl shadow-ink/10 outline-none sm:max-h-[90vh] sm:max-w-2xl sm:rounded-lg sm:border sm:p-6"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 id={headingID} className="text-lg font-bold text-ink">
            {title}
          </h2>
          <button
            type="button"
            onClick={requestClose}
            aria-label="Close"
            className="rounded text-graphite outline-none transition hover:text-ink focus-visible:ring-2 focus-visible:ring-blueprint"
          >
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

const FIRST_FIELD = "input:not([disabled]):not([type=hidden]), textarea:not([disabled])";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
