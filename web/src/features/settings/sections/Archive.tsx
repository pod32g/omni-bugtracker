import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { Card, ErrorLine, primaryButtonClass } from "../ui";

export function ArchiveSection() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ["archive-settings"], queryFn: () => api.getArchiveSettings() });
  const [enabled, setEnabled] = useState(false);
  const [days, setDays] = useState("30");

  // Seed the form from the current setting (auto_after_days: 0 = disabled).
  useEffect(() => {
    if (!settings.data) return;
    const d = settings.data.auto_after_days;
    setEnabled(d > 0);
    if (d > 0) setDays(String(d));
  }, [settings.data]);

  const save = useMutation({
    mutationFn: (autoAfterDays: number) => api.setArchiveSettings(autoAfterDays),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["archive-settings"] }),
  });

  const parsedDays = Math.max(1, parseInt(days, 10) || 0);
  const apply = () => save.mutate(enabled ? parsedDays : 0);

  return (
    <Card
      title="Auto-archive"
      description={
        <>
          Automatically archive issues that have stayed closed for a while — they drop out of lists and search but
          stay recoverable under the <span className="font-mono text-xs">is:archived</span> filter. Runs once a day.
        </>
      }
    >
      <label className="flex items-center gap-2.5 text-sm text-ink">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          className="h-4 w-4 accent-blueprint"
        />
        Enable auto-archive
      </label>

      <div className={`flex items-center gap-2 text-sm ${enabled ? "text-ink" : "pointer-events-none opacity-50"}`}>
        <span>Archive issues closed more than</span>
        <input
          type="number"
          min={1}
          value={days}
          disabled={!enabled}
          onChange={(e) => setDays(e.target.value)}
          className="w-20 rounded-md border border-hairline bg-paper px-2.5 py-1.5 text-sm text-ink outline-none focus:border-blueprint"
        />
        <span>days ago.</span>
      </div>

      <ErrorLine error={save.error} />
      <div className="flex items-center gap-3">
        <button onClick={apply} disabled={save.isPending || settings.isLoading} className={primaryButtonClass}>
          {save.isPending ? "Saving…" : "Save changes"}
        </button>
        <span className="text-sm text-graphite-soft">
          {settings.data == null
            ? ""
            : settings.data.auto_after_days > 0
              ? `Currently archiving issues closed over ${settings.data.auto_after_days} days ago.`
              : "Currently disabled."}
        </span>
      </div>
    </Card>
  );
}
