import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api, type IssueStatus } from "../../lib/api";
import { PriorityBadge, SeverityBadge, StatusBadge } from "../../components/Badges";
import { EditIssueForm } from "./EditIssueForm";

const TRANSITIONS: IssueStatus[] = [
  "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened",
];

export function IssueDetail() {
  const { issueKey = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [comment, setComment] = useState("");
  const [editing, setEditing] = useState(false);

  const issue = useQuery({ queryKey: ["issue", issueKey], queryFn: () => api.getIssue(issueKey) });
  const comments = useQuery({ queryKey: ["comments", issueKey], queryFn: () => api.listComments(issueKey) });
  const activity = useQuery({ queryKey: ["activity", issueKey], queryFn: () => api.activity(issueKey) });
  const commits = useQuery({ queryKey: ["commits", issueKey], queryFn: () => api.commits(issueKey) });

  const transition = useMutation({
    mutationFn: (to: IssueStatus) => api.transition(issueKey, to),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", issueKey] }),
  });
  const del = useMutation({
    mutationFn: () => api.deleteIssue(issueKey),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issues"] });
      navigate("/issues");
    },
  });
  const addComment = useMutation({
    mutationFn: () => api.addComment(issueKey, comment),
    onSuccess: () => {
      setComment("");
      qc.invalidateQueries({ queryKey: ["comments", issueKey] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    },
  });

  if (issue.isLoading) return <div className="text-slate-400">Loading…</div>;
  if (issue.isError || !issue.data)
    return <div className="text-severity-high">{(issue.error as Error)?.message ?? "Not found"}</div>;

  const i = issue.data;

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_16rem]">
      <div>
        <div className="mb-2 flex items-center gap-2 text-sm">
          <span className="font-mono text-accent-hover">{i.key}</span>
          <StatusBadge status={i.status} />
          <SeverityBadge severity={i.severity} />
          <PriorityBadge priority={i.priority} />
          <div className="ml-auto flex gap-2">
            <button
              onClick={() => setEditing(true)}
              className="rounded-lg border border-surface-border px-2.5 py-1 text-xs text-slate-300 hover:border-accent hover:text-accent-hover"
            >
              Edit
            </button>
            <button
              onClick={() => {
                if (window.confirm(`Delete ${i.key}? This removes it from lists (soft delete).`)) del.mutate();
              }}
              disabled={del.isPending}
              className="rounded-lg border border-surface-border px-2.5 py-1 text-xs text-severity-high hover:border-severity-high disabled:opacity-50"
            >
              {del.isPending ? "Deleting…" : "Delete"}
            </button>
          </div>
        </div>
        <h1 className="mb-4 text-2xl font-semibold">{i.title}</h1>

        <Section title="Description" body={i.description_md} />
        {i.repro_steps_md && <Section title="Reproduction Steps" body={i.repro_steps_md} />}
        {i.expected_md && <Section title="Expected Behavior" body={i.expected_md} />}
        {i.actual_md && <Section title="Actual Behavior" body={i.actual_md} />}
        {i.environment_md && <Section title="Environment" body={i.environment_md} />}

        <h2 className="mb-3 mt-8 text-sm font-medium text-slate-300">Comments</h2>
        <div className="space-y-3">
          {comments.data?.map((c) => (
            <div key={c.id} className="rounded-lg border border-surface-border bg-surface-raised p-4">
              <div className="mb-1 text-xs text-slate-500">
                {c.author?.display_name ?? "unknown"} · {new Date(c.created_at).toLocaleString()}
              </div>
              <div className="markdown">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{c.body_md}</ReactMarkdown>
              </div>
            </div>
          ))}
        </div>

        <div className="mt-4">
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Add a comment (Markdown supported)…"
            className="w-full rounded-lg border border-surface-border bg-surface-raised px-3 py-2 text-sm outline-none focus:border-accent"
            rows={3}
          />
          <button
            disabled={!comment.trim() || addComment.isPending}
            onClick={() => addComment.mutate()}
            className="mt-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-50"
          >
            Comment
          </button>
        </div>
      </div>

      <aside className="space-y-6">
        <div>
          <h3 className="mb-2 text-xs uppercase tracking-wide text-slate-500">Transition</h3>
          <div className="flex flex-wrap gap-2">
            {TRANSITIONS.map((to) => (
              <button
                key={to}
                onClick={() => transition.mutate(to)}
                className="rounded-lg border border-surface-border px-2.5 py-1 text-xs text-slate-300 hover:border-accent hover:text-accent-hover"
              >
                {to.replace(/_/g, " ")}
              </button>
            ))}
          </div>
        </div>
        <div>
          <h3 className="mb-2 text-xs uppercase tracking-wide text-slate-500">Assignee</h3>
          <p className="text-sm text-slate-300">{i.assignee?.display_name ?? "Unassigned"}</p>
        </div>
        <div>
          <h3 className="mb-2 text-xs uppercase tracking-wide text-slate-500">Development</h3>
          {commits.data && commits.data.length > 0 ? (
            <ul className="space-y-2 text-xs">
              {commits.data.map((c) => (
                <li key={c.sha}>
                  <a
                    href={c.url}
                    target="_blank"
                    rel="noreferrer"
                    className="font-mono text-accent-hover hover:underline"
                  >
                    {c.sha.slice(0, 7)}
                  </a>
                  <span className="ml-1 rounded bg-white/5 px-1 text-slate-400">{c.verb}</span>
                  <div className="truncate text-slate-500">{c.message.split("\n")[0]}</div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-slate-500">No linked commits yet.</p>
          )}
        </div>
        <div>
          <h3 className="mb-2 text-xs uppercase tracking-wide text-slate-500">Activity</h3>
          <ul className="space-y-2 text-xs text-slate-400">
            {activity.data?.map((a) => (
              <li key={a.id}>
                <span className="text-slate-300">{a.verb}</span>
                <br />
                {new Date(a.occurred_at).toLocaleString()}
              </li>
            ))}
          </ul>
        </div>
      </aside>

      {editing && <EditIssueForm issue={i} onClose={() => setEditing(false)} />}
    </div>
  );
}

function Section({ title, body }: { title: string; body: string }) {
  return (
    <div className="mb-5">
      <h2 className="mb-2 text-sm font-medium text-slate-300">{title}</h2>
      <div className="markdown rounded-lg border border-surface-border bg-surface-raised p-4">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{body || "_No content_"}</ReactMarkdown>
      </div>
    </div>
  );
}
