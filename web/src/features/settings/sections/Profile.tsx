import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../../lib/api";
import { Avatar } from "../../../components/Badges";
import { Card, ErrorLine, fieldLabelClass, inputClass, primaryButtonClass } from "../ui";

/**
 * ProfileSection edits the two fields a person owns about themselves. Everything else
 * on the account is somebody else's to set: the email comes from Omni-Identity, and
 * the role is an admin's decision — so both are shown, and neither is a form field.
 */
export function ProfileSection() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const [displayName, setDisplayName] = useState("");
  const [avatarURL, setAvatarURL] = useState("");
  const [saved, setSaved] = useState(false);

  // Seed once the account has loaded, and re-seed after a save so the form shows
  // exactly what the server stored (trimmed, in particular).
  useEffect(() => {
    if (!me.data) return;
    setDisplayName(me.data.display_name ?? "");
    setAvatarURL(me.data.avatar_url ?? "");
  }, [me.data]);

  const save = useMutation({
    mutationFn: () => api.updateMe({ display_name: displayName.trim(), avatar_url: avatarURL.trim() }),
    onSuccess: (user) => {
      // Every byline in the app reads from these two queries.
      qc.setQueryData(["me"], user);
      qc.invalidateQueries({ queryKey: ["users"] });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    },
  });

  const dirty =
    !!me.data && (displayName.trim() !== (me.data.display_name ?? "") || avatarURL.trim() !== (me.data.avatar_url ?? ""));
  const preview = { ...(me.data ?? { id: "", email: "", display_name: "" }), display_name: displayName, avatar_url: avatarURL };

  return (
    <Card
      title="Profile"
      description="How you appear on issues, comments and mentions. Your name and avatar start out mirrored from Omni-Identity; once you change them here, signing in again leaves them alone."
    >
      <div className="flex items-center gap-4">
        <Avatar user={preview} size={56} />
        <div className="min-w-0">
          <div className="truncate text-sm font-medium text-ink">{displayName || me.data?.email || "—"}</div>
          <div className="truncate font-mono text-xs text-graphite-soft">
            {me.data?.email}
            {me.data?.role && ` · ${me.data.role}`}
          </div>
        </div>
      </div>

      <label className="text-sm">
        <span className={fieldLabelClass}>Display name</span>
        <input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && dirty && displayName.trim()) save.mutate();
          }}
          maxLength={80}
          placeholder="Your name"
          className={`w-full max-w-sm ${inputClass}`}
        />
      </label>

      <label className="text-sm">
        <span className={fieldLabelClass}>Avatar URL</span>
        <input
          value={avatarURL}
          onChange={(e) => setAvatarURL(e.target.value)}
          placeholder="https://… (empty falls back to your initials)"
          className={`w-full max-w-sm ${inputClass}`}
        />
      </label>

      <p className="text-sm text-graphite-soft">
        Email and role aren't editable here — the first comes from Omni-Identity, the second from an admin.
      </p>

      <ErrorLine error={save.error} />
      <div className="flex items-center gap-3">
        <button
          onClick={() => save.mutate()}
          disabled={!dirty || !displayName.trim() || save.isPending}
          className={primaryButtonClass}
        >
          {save.isPending ? "Saving…" : "Save profile"}
        </button>
        {saved && !dirty && <span className="text-sm text-resolved">Saved.</span>}
      </div>
    </Card>
  );
}
