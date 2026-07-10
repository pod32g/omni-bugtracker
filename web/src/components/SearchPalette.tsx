import { useEffect, useRef, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, type SearchHit } from "../lib/api";
import { StatusPill } from "./Badges";
import { IconSearch } from "./icons";

// Command-palette-style global search (⌘K / "/"). Queries Postgres FTS across
// issues and comments in every project.
export function SearchPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);
  const [q, setQ] = useState("");
  const [dq, setDq] = useState(""); // debounced
  const [sel, setSel] = useState(0);

  useEffect(() => {
    const t = setTimeout(() => setDq(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);

  useEffect(() => {
    if (open) {
      setQ("");
      setDq("");
      setSel(0);
      // Focus after the overlay mounts.
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  const results = useQuery({
    queryKey: ["search", dq],
    queryFn: () => api.search(dq),
    enabled: open && dq.length >= 2,
    placeholderData: keepPreviousData,
  });

  if (!open) return null;
  const items = dq.length >= 2 ? (results.data?.items ?? []) : [];

  const pick = (hit: SearchHit) => {
    onClose();
    navigate(`/issues/${hit.issue_key}`);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-ink/40 p-4 pt-[12vh]" onClick={onClose}>
      <div
        className="flex w-full max-w-xl flex-col overflow-hidden rounded-lg border border-hairline bg-paper shadow-xl shadow-ink/20"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-hairline px-4">
          <IconSearch size={16} className="shrink-0 text-graphite-soft" />
          <input
            ref={inputRef}
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setSel(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") onClose();
              else if (e.key === "ArrowDown") {
                e.preventDefault();
                setSel((s) => Math.min(s + 1, items.length - 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setSel((s) => Math.max(s - 1, 0));
              } else if (e.key === "Enter" && items[sel]) {
                pick(items[sel]);
              }
            }}
            placeholder="Search issues and comments across projects…"
            className="h-12 grow bg-transparent text-sm text-ink outline-none placeholder:text-graphite-soft"
          />
          <kbd className="shrink-0 rounded border border-hairline bg-panel px-1.5 py-0.5 font-mono text-[10px] text-graphite-soft">
            esc
          </kbd>
        </div>

        <div className="max-h-[50vh] overflow-y-auto">
          {dq.length < 2 && (
            <p className="px-4 py-6 text-center text-sm text-graphite-soft">
              Type at least 2 characters — supports quoted phrases and -exclusions.
            </p>
          )}
          {dq.length >= 2 && results.isSuccess && items.length === 0 && (
            <p className="px-4 py-6 text-center text-sm text-graphite-soft">No matches for “{dq}”.</p>
          )}
          {items.map((hit, idx) => (
            <button
              key={`${hit.issue_key}-${hit.matched_in}-${idx}`}
              onClick={() => pick(hit)}
              onMouseEnter={() => setSel(idx)}
              className={`flex w-full flex-col gap-1 border-b border-hairline px-4 py-3 text-left transition last:border-b-0 ${
                idx === sel ? "bg-blueprint-soft/60" : ""
              }`}
            >
              <span className="flex items-center gap-2.5">
                <span className="shrink-0 font-mono text-xs font-semibold text-blueprint">{hit.issue_key}</span>
                <span className="truncate text-sm font-medium text-ink">{hit.title}</span>
                <span className="grow" />
                {hit.matched_in === "comment" && (
                  <span className="shrink-0 rounded-full bg-panel px-2 py-0.5 font-mono text-[10px] uppercase tracking-caps text-graphite">
                    comment
                  </span>
                )}
                <StatusPill status={hit.status} />
              </span>
              <span className="line-clamp-2 text-xs leading-relaxed text-graphite">
                <Snippet text={hit.snippet} />
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

// Snippets arrive as plain text with «…» marking matches (never HTML — the
// underlying text is user content). Render marks as bold nodes.
function Snippet({ text }: { text: string }) {
  const parts = text.split(/«([^»]*)»/g);
  return (
    <>
      {parts.map((p, i) =>
        i % 2 === 1 ? (
          <b key={i} className="font-semibold text-ink">
            {p}
          </b>
        ) : (
          p
        ),
      )}
    </>
  );
}
