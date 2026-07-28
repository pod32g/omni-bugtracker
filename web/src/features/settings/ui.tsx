import type { ReactNode } from "react";

// Shared chrome for settings sections. Every section was repeating the same card
// shell and the same input classes, which is how they drifted apart — three of them
// had a bold heading and the rest semibold.

export function Card({
  title,
  description,
  actions,
  tone,
  children,
}: {
  title?: string;
  description?: ReactNode;
  /** Rendered top-right of the header row, e.g. a filter select. */
  actions?: ReactNode;
  tone?: "critical";
  children: ReactNode;
}) {
  return (
    <section
      className={`flex flex-col gap-4 rounded-lg border bg-paper p-6 ${
        tone === "critical" ? "border-critical/40" : "border-hairline"
      }`}
    >
      {(title || description || actions) && (
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            {title && (
              <h2 className={`text-base font-semibold ${tone === "critical" ? "text-critical" : "text-ink"}`}>
                {title}
              </h2>
            )}
            {description && <p className="text-sm leading-relaxed text-graphite">{description}</p>}
          </div>
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export const inputClass =
  "rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint";

export const primaryButtonClass =
  "rounded-md bg-blueprint px-4 py-2 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50";

export const quietButtonClass =
  "shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50";

export const dangerButtonClass =
  "shrink-0 rounded-md border border-hairline px-3 py-1.5 text-sm text-critical transition hover:border-critical disabled:opacity-50";

export const fieldLabelClass =
  "mb-1 block font-mono text-[10px] uppercase tracking-caps text-graphite-soft";

/** Inline error line — every section reports mutation failures the same way. */
export function ErrorLine({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="text-sm text-critical">{(error as Error).message}</p>;
}
