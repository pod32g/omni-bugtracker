import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { timeAgo } from "../../../lib/activity";
import { Card, ErrorLine } from "../ui";

/**
 * AuditSection is the admin view of the append-only log. Read-only by construction —
 * there is no edit or delete path on the server either, because a log somebody can
 * quietly amend is not evidence of anything.
 */
export function AuditSection() {
  const [action, setAction] = useState("");
  const log = useQuery({ queryKey: ["audit", action], queryFn: () => api.audit({ action: action || undefined }) });

  const items = log.data?.items ?? [];

  return (
    <Card
      title="Audit log"
      description="Privileged actions: role changes, tokens, project and membership changes, webhooks, automation rules, settings. Append-only."
      actions={
        <select
          value={action}
          onChange={(e) => setAction(e.target.value)}
          aria-label="Filter by action"
          className="h-9 rounded-md border border-hairline bg-paper px-2.5 text-sm text-ink outline-none focus:border-blueprint"
        >
          <option value="">All actions</option>
          {(log.data?.actions ?? []).map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
      }
    >
      <ErrorLine error={log.error} />
      {log.isSuccess && items.length === 0 && <p className="text-sm text-graphite-soft">Nothing recorded yet.</p>}

      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-left text-sm">
            <thead>
              <tr className="border-b border-hairline font-mono text-[10px] uppercase tracking-caps text-graphite-soft">
                <th className="py-2 pr-3 font-medium">When</th>
                <th className="py-2 pr-3 font-medium">Who</th>
                <th className="py-2 pr-3 font-medium">Action</th>
                <th className="py-2 pr-3 font-medium">Target</th>
                <th className="py-2 font-medium">Detail</th>
              </tr>
            </thead>
            <tbody>
              {items.map((e) => (
                <tr key={e.id} className="border-b border-hairline align-top last:border-b-0">
                  <td className="whitespace-nowrap py-2 pr-3 font-mono text-xs text-graphite-soft">
                    {timeAgo(e.created_at)}
                  </td>
                  <td className="py-2 pr-3">
                    <span className="text-ink">{e.actor_name || e.actor_email}</span>
                    {/* A privileged change made by a script is a different fact from one
                        made in a browser, so the log says which. */}
                    {e.via_token && (
                      <span className="ml-1.5 rounded-sm bg-panel px-1 py-0.5 font-mono text-[10px] text-graphite-soft">
                        token
                      </span>
                    )}
                  </td>
                  <td className="py-2 pr-3 font-mono text-xs text-blueprint">{e.action}</td>
                  <td className="py-2 pr-3 text-graphite">{e.target_label || e.target_id || e.target_type}</td>
                  <td className="py-2 font-mono text-xs text-graphite-soft">
                    {e.details && Object.keys(e.details).length > 0 ? JSON.stringify(e.details) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}
