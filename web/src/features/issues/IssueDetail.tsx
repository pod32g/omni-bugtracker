import { useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api, UNASSIGNED, type IssueStatus, type NewIssue, type Priority, type Project, type User } from "../../lib/api";
import { humanizeVerb, timeAgo } from "../../lib/activity";
import { Avatar, LabelChip, PriorityText, SeverityMark, SeverityPill, StatusPill, statusLabel, statusTone } from "../../components/Badges";
import { IconBranch, IconChevronDown, IconCommit, IconKebab, IconMilestone, IconPencil } from "../../components/icons";
import { EditIssueForm } from "./EditIssueForm";

const TRANSITIONS: IssueStatus[] = [
  "open", "in_progress", "blocked", "ready_for_review", "resolved", "closed", "reopened",
];

export function IssueDetail() {
  const { issueKey = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [comment, setComment] = useState("");
  const [editing, setEditing] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  const issue = useQuery({ queryKey: ["issue", issueKey], queryFn: () => api.getIssue(issueKey) });
  const comments = useQuery({ queryKey: ["comments", issueKey], queryFn: () => api.listComments(issueKey) });
  const activity = useQuery({ queryKey: ["activity", issueKey], queryFn: () => api.activity(issueKey) });
  const commits = useQuery({ queryKey: ["commits", issueKey], queryFn: () => api.commits(issueKey) });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });

  // Inline quick-edit of rail fields (assignee, priority), à la Jira.
  const patch = useMutation({
    mutationFn: (body: Partial<NewIssue>) => api.updateIssue(issueKey, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issueKey] });
      qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });

  // Moving reallocates the issue's number in the target project, so its key changes —
  // navigate to the new key on success. Both project lists refetch via the ["issues"] prefix.
  const move = useMutation({
    mutationFn: (targetProjectKey: string) => api.moveIssue(issueKey, targetProjectKey),
    onSuccess: (moved) => {
      qc.invalidateQueries({ queryKey: ["issues"] });
      qc.invalidateQueries({ queryKey: ["issue", moved.key] });
      navigate(`/issues/${moved.key}`, { replace: true });
    },
  });

  const transition = useMutation({
    mutationFn: (to: IssueStatus) => api.transition(issueKey, to),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issueKey] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    },
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
      if (composerRef.current) composerRef.current.style.height = ""; // reset the auto-grown height
      qc.invalidateQueries({ queryKey: ["comments", issueKey] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    },
  });

  if (issue.isLoading) return <div className="px-9 py-10 text-sm text-graphite">Loading…</div>;
  if (issue.isError || !issue.data)
    return <div className="px-9 py-10 text-sm text-critical">{(issue.error as Error)?.message ?? "Not found"}</div>;

  const i = issue.data;

  return (
    <div>
      {/* Topbar */}
      <div className="sticky top-0 z-10 flex h-[60px] items-center justify-between border-b border-hairline bg-paper/80 px-8 backdrop-blur">
        <div className="flex items-center gap-2.5 text-sm">
          <Link to="/issues" className="text-graphite transition hover:text-ink">
            Issues
          </Link>
          <span className="text-graphite-soft">›</span>
          <span className="font-mono font-medium text-blueprint">{i.key}</span>
        </div>
        <div className="relative flex items-center gap-2.5">
          <button
            onClick={() => setEditing(true)}
            className="flex h-[34px] items-center gap-1.5 rounded-md border border-hairline px-3.5 text-sm font-semibold text-ink transition hover:border-graphite"
          >
            <IconPencil size={14} className="text-graphite" />
            Edit
          </button>
          <button
            onClick={() => setMenuOpen((s) => !s)}
            className="grid h-[34px] w-[34px] place-items-center rounded-md border border-hairline text-graphite transition hover:border-graphite hover:text-ink"
            aria-label="More actions"
          >
            <IconKebab size={16} />
          </button>
          {menuOpen && (
            <>
              <button className="fixed inset-0 z-10 cursor-default" aria-hidden onClick={() => setMenuOpen(false)} />
              <div className="absolute right-0 top-[38px] z-20 w-44 rounded-md border border-hairline bg-paper py-1 shadow-lg shadow-ink/5">
                <button
                  onClick={() => {
                    setMenuOpen(false);
                    if (window.confirm(`Delete ${i.key}? This removes it from lists (soft delete).`)) del.mutate();
                  }}
                  disabled={del.isPending}
                  className="block w-full px-3 py-2 text-left text-sm text-critical transition hover:bg-critical-soft disabled:opacity-50"
                >
                  {del.isPending ? "Deleting…" : "Delete issue"}
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      {/* Body */}
      <div className="mx-auto grid w-full max-w-[1160px] lg:grid-cols-[minmax(0,1fr)_320px]">
        <article className="flex flex-col gap-8 px-6 py-9 sm:px-8 lg:px-10">
          <header className="flex flex-col gap-3.5">
            <div className="flex flex-wrap items-center gap-2.5">
              <SeverityPill severity={i.severity} />
              <StatusPill status={i.status} />
              <span className="rounded-sm border border-hairline bg-panel px-2.5 py-1 font-mono text-xs font-semibold text-ink">
                {i.priority.toUpperCase()}
              </span>
            </div>
            <h1 className="text-[28px] font-bold leading-[1.15] tracking-[-0.02em] text-ink">{i.title}</h1>
            <div className="flex items-center gap-2">
              <Avatar user={i.reporter} size={22} />
              <p className="text-sm text-graphite">
                {i.reporter?.display_name ?? i.reporter?.email ?? "Someone"} opened this · {timeAgo(i.created_at)} · #
                {i.number}
              </p>
            </div>
          </header>

          <Section title="Description">
            <Markdown body={i.description_md} />
          </Section>

          {i.repro_steps_md && (
            <Section title="Steps to reproduce">
              <Markdown body={i.repro_steps_md} />
            </Section>
          )}

          {(i.expected_md || i.actual_md) && (
            <div className="flex flex-col gap-4 sm:flex-row">
              {i.expected_md && <Callout tone="resolved" label="Expected" body={i.expected_md} />}
              {i.actual_md && <Callout tone="critical" label="Actual" body={i.actual_md} />}
            </div>
          )}

          {i.environment_md && (
            <Section title="Environment">
              <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-terminal px-4 py-3.5 font-mono text-sm leading-[1.6] text-terminal-ink">
                {i.environment_md}
              </pre>
            </Section>
          )}

          {/* Comments */}
          <div className="flex flex-col gap-4 border-t border-hairline pt-6">
            <MicroLabel>Comments</MicroLabel>
            {comments.data?.map((c) => (
              <div key={c.id} className="flex gap-3">
                <Avatar user={c.author} size={28} />
                <div className="flex min-w-0 grow flex-col gap-1 rounded-md border border-hairline bg-paper p-3.5">
                  <div className="text-xs text-graphite-soft">
                    <span className="font-medium text-ink">{c.author?.display_name ?? "unknown"}</span> ·{" "}
                    {timeAgo(c.created_at)}
                  </div>
                  <div className="markdown">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{c.body_md}</ReactMarkdown>
                  </div>
                </div>
              </div>
            ))}

            <div className="flex items-start gap-3">
              <Avatar user={i.reporter} size={28} />
              <textarea
                ref={composerRef}
                value={comment}
                onChange={(e) => {
                  setComment(e.target.value);
                  const t = e.currentTarget;
                  t.style.height = "auto";
                  t.style.height = `${Math.min(t.scrollHeight, 176)}px`; // grow with content, cap at ~176px
                }}
                placeholder="Leave a comment… (Markdown supported)"
                rows={2}
                className="max-h-44 grow resize-none overflow-y-auto rounded-md border border-hairline bg-paper px-3.5 py-2.5 text-sm text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
              />
              <button
                disabled={!comment.trim() || addComment.isPending}
                onClick={() => addComment.mutate()}
                className="h-11 shrink-0 rounded-md bg-blueprint px-4.5 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
              >
                Comment
              </button>
            </div>
          </div>
        </article>

        {/* Meta rail — full-height panel; content sticks while scrolling the doc. */}
        <aside className="border-hairline bg-mist lg:border-l">
          <div className="flex flex-col gap-6 px-6 py-8 lg:sticky lg:top-[60px] lg:max-h-[calc(100vh-60px)] lg:overflow-y-auto">
          <div className="flex flex-col gap-2">
            <MicroLabel>Status</MicroLabel>
            <StatusControl status={i.status} onChange={(to) => transition.mutate(to)} pending={transition.isPending} />
          </div>

          <MetaRow label="Project">
            <ProjectControl
              projectKey={i.project_key}
              projects={(projects.data?.items ?? []).filter((p) => p.key !== i.project_key)}
              pending={move.isPending}
              onChange={(key) => {
                if (
                  window.confirm(
                    `Move ${i.key} to ${key}? It gets a new key (${key}-N) and loses its milestone, release, and components.`,
                  )
                )
                  move.mutate(key);
              }}
            />
          </MetaRow>

          <MetaRow label="Assignee">
            <AssigneeControl
              assignee={i.assignee}
              users={users.data?.items ?? []}
              onChange={(assignee_id) => patch.mutate({ assignee_id })}
            />
          </MetaRow>

          {i.labels && i.labels.length > 0 && (
            <MetaRow label="Labels">
              <div className="flex flex-wrap gap-1.5">
                {i.labels.map((l) => (
                  <LabelChip key={l} name={l} />
                ))}
              </div>
            </MetaRow>
          )}

          <div className="flex gap-4">
            <MetaRow label="Priority" className="grow">
              <PriorityControl priority={i.priority} onChange={(priority) => patch.mutate({ priority })} />
            </MetaRow>
            <MetaRow label="Severity" className="grow">
              <SeverityMark severity={i.severity} />
            </MetaRow>
          </div>

          {(i.version_fixed || i.version_affected) && (
            <MetaRow label="Version">
              <div className="flex items-center gap-2 text-sm font-medium text-ink">
                <IconMilestone size={15} className="text-graphite" />
                {i.version_fixed ? `${i.version_fixed} — fixed` : `${i.version_affected} — affected`}
              </div>
            </MetaRow>
          )}

          <div className="flex flex-col gap-3 border-t border-hairline pt-5">
            <MicroLabel>Development</MicroLabel>
            {commits.data && commits.data.length > 0 ? (
              commits.data.map((c) => (
                <a key={c.sha} href={c.url} target="_blank" rel="noreferrer" className="flex items-center gap-2.5">
                  <span className="grid h-6 w-6 shrink-0 place-items-center rounded-sm bg-panel text-graphite">
                    {c.verb.includes("pr") ? <IconBranch size={13} /> : <IconCommit size={13} />}
                  </span>
                  <span className="flex min-w-0 flex-col">
                    <span className="font-mono text-sm font-medium text-blueprint hover:underline">
                      {c.sha.slice(0, 7)}
                    </span>
                    <span className="truncate font-mono text-xs text-graphite-soft">
                      {c.message.split("\n")[0]}
                    </span>
                  </span>
                </a>
              ))
            ) : (
              <p className="text-xs text-graphite-soft">No linked commits yet.</p>
            )}
          </div>

          {activity.data && activity.data.length > 0 && (
            <div className="flex flex-col gap-2.5 border-t border-hairline pt-5">
              <MicroLabel>Activity</MicroLabel>
              <ul className="flex flex-col gap-2">
                {activity.data.map((a) => (
                  <li key={a.id} className="text-xs text-graphite">
                    <span className="font-medium text-ink">{a.actor?.display_name ?? "system"}</span>{" "}
                    {humanizeVerb(a.verb)}
                    <span className="text-graphite-soft"> · {timeAgo(a.occurred_at)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}
          </div>
        </aside>
      </div>

      {editing && <EditIssueForm issue={i} onClose={() => setEditing(false)} />}
    </div>
  );
}

function MicroLabel({ children }: { children: ReactNode }) {
  return (
    <span className="font-mono text-[10px] font-medium uppercase tracking-caps text-graphite-soft">{children}</span>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3 border-t border-hairline pt-6">
      <MicroLabel>{title}</MicroLabel>
      {children}
    </section>
  );
}

function Markdown({ body }: { body?: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{body || "_No content_"}</ReactMarkdown>
    </div>
  );
}

function Callout({ tone, label, body }: { tone: "resolved" | "critical"; label: string; body: string }) {
  const border = tone === "resolved" ? "border-l-resolved" : "border-l-critical";
  const text = tone === "resolved" ? "text-resolved" : "text-critical";
  return (
    <div className={`flex grow basis-0 flex-col gap-2 rounded-md border-l-[3px] bg-panel/50 px-4 py-3.5 ${border}`}>
      <span className={`font-mono text-[10px] font-medium uppercase tracking-caps ${text}`}>{label}</span>
      <div className="markdown text-[14px] leading-[1.55]">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{body}</ReactMarkdown>
      </div>
    </div>
  );
}

function MetaRow({ label, children, className = "" }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={`flex flex-col gap-2 ${className}`}>
      <MicroLabel>{label}</MicroLabel>
      {children}
    </div>
  );
}

function StatusControl({
  status,
  onChange,
  pending,
}: {
  status: IssueStatus;
  onChange: (to: IssueStatus) => void;
  pending: boolean;
}) {
  const t = statusTone[status];
  return (
    <div className="relative">
      <div className={`flex h-[38px] items-center justify-between rounded-md border px-3 ${t.bg} ${t.border}`}>
        <span className="flex items-center gap-2">
          <span className={`h-1.5 w-1.5 rounded-full ${t.dot}`} />
          <span className={`text-sm font-semibold ${t.text}`}>{pending ? "Updating…" : statusLabel[status]}</span>
        </span>
        <IconChevronDown size={14} className={t.text} />
      </div>
      <select
        value={status}
        onChange={(e) => e.target.value !== status && onChange(e.target.value as IssueStatus)}
        aria-label="Change status"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        {TRANSITIONS.map((s) => (
          <option key={s} value={s}>
            {statusLabel[s]}
          </option>
        ))}
      </select>
    </div>
  );
}

function AssigneeControl({
  assignee,
  users,
  onChange,
}: {
  assignee?: User;
  users: User[];
  onChange: (assigneeId: string) => void;
}) {
  return (
    <div className="relative">
      <div className="-mx-1.5 flex items-center gap-2.5 rounded-md px-1.5 py-1 transition hover:bg-panel">
        <Avatar user={assignee} size={26} />
        <span className="text-sm font-medium text-ink">
          {assignee?.display_name ?? assignee?.email ?? "Unassigned"}
        </span>
      </div>
      <select
        value={assignee?.id ?? UNASSIGNED}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Change assignee"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        <option value={UNASSIGNED}>Unassigned</option>
        {users.map((u) => (
          <option key={u.id} value={u.id}>
            {u.display_name || u.email}
          </option>
        ))}
      </select>
    </div>
  );
}

function ProjectControl({
  projectKey,
  projects,
  pending,
  onChange,
}: {
  projectKey: string;
  projects: Project[];
  pending: boolean;
  onChange: (key: string) => void;
}) {
  return (
    <div className="relative">
      <div className="-mx-1.5 flex items-center justify-between gap-2 rounded-md px-1.5 py-1 transition hover:bg-panel">
        <span className="flex items-center gap-2">
          <span className="rounded-sm border border-hairline bg-panel px-1.5 py-0.5 font-mono text-xs font-semibold text-blueprint">
            {projectKey}
          </span>
          {pending && <span className="text-sm text-graphite-soft">Moving…</span>}
        </span>
        <IconChevronDown size={12} className="text-graphite-soft" />
      </div>
      <select
        value={projectKey}
        onChange={(e) => e.target.value !== projectKey && onChange(e.target.value)}
        aria-label="Move to project"
        disabled={pending || projects.length === 0}
        className="absolute inset-0 cursor-pointer opacity-0 disabled:cursor-not-allowed"
      >
        <option value={projectKey}>{projectKey} (current)</option>
        {projects.map((p) => (
          <option key={p.key} value={p.key}>
            {p.key} — {p.name}
          </option>
        ))}
      </select>
    </div>
  );
}

const PRIORITIES: Priority[] = ["p0", "p1", "p2", "p3"];

function PriorityControl({ priority, onChange }: { priority: Priority; onChange: (p: Priority) => void }) {
  return (
    <div className="relative inline-block">
      <div className="-mx-1.5 flex items-center gap-1 rounded-md px-1.5 py-1 transition hover:bg-panel">
        <PriorityText priority={priority} />
        <IconChevronDown size={12} className="text-graphite-soft" />
      </div>
      <select
        value={priority}
        onChange={(e) => onChange(e.target.value as Priority)}
        aria-label="Change priority"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        {PRIORITIES.map((p) => (
          <option key={p} value={p}>
            {p.toUpperCase()}
          </option>
        ))}
      </select>
    </div>
  );
}
