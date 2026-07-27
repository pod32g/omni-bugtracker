import { useEffect, useRef, useState, type DragEvent, type ReactNode } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  api,
  UNASSIGNED,
  type Comment,
  type Issue,
  type IssueSLA,
  EMOJI,
  type IssueStatus,
  type Milestone,
  type NewIssue,
  type Priority,
  type Project,
  type RelationKind,
  type Release,
  type User,
} from "../../lib/api";
import { describeActivity, timeAgo } from "../../lib/activity";
import { remarkIssueKeys } from "../../lib/issueRefs";
import { matchUsers, mentionQuery, preferredHandle, remarkMentions } from "../../lib/mentions";
import {
  Avatar,
  DueChip,
  LabelChip,
  PriorityText,
  relativeDue,
  SeverityMark,
  SeverityPill,
  SLAPill,
  StatusPill,
  statusLabel,
  statusTone,
} from "../../components/Badges";
import { IconBranch, IconChevronDown, IconCommit, IconEye, IconKebab, IconMilestone, IconPencil } from "../../components/icons";
import { EditIssueForm } from "./EditIssueForm";
import { ComponentsSelect } from "./formFields";

const COMMENT_PAGE_SIZE = 50;
const ACTIVITY_PAGE_SIZE = 50;

// Mirrors domain.validTransitions on the server. Offering statuses the workflow
// rejects turns every mis-click into a 409, so the picker only lists legal targets.
const ALLOWED_TRANSITIONS: Record<IssueStatus, IssueStatus[]> = {
  open: ["in_progress", "blocked", "resolved", "closed"],
  in_progress: ["blocked", "ready_for_review", "resolved", "open"],
  blocked: ["in_progress", "open", "closed"],
  ready_for_review: ["in_progress", "resolved", "closed"],
  resolved: ["closed", "reopened"],
  closed: ["reopened"],
  reopened: ["in_progress", "resolved", "closed", "blocked"],
};

export function IssueDetail() {
  const { issueKey = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [comment, setComment] = useState("");
  const [editing, setEditing] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  const issue = useQuery({ queryKey: ["issue", issueKey], queryFn: () => api.getIssue(issueKey) });
  // Comments and activity page rather than truncate: a long-running issue has more
  // history than one request returns, and silently dropping the tail hides context.
  const comments = useInfiniteQuery({
    queryKey: ["comments", issueKey],
    queryFn: ({ pageParam }) => api.listComments(issueKey, COMMENT_PAGE_SIZE, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const attachments = useQuery({ queryKey: ["attachments", issueKey], queryFn: () => api.listAttachments(issueKey) });
  const activity = useInfiniteQuery({
    queryKey: ["activity", issueKey],
    queryFn: ({ pageParam }) => api.activity(issueKey, ACTIVITY_PAGE_SIZE, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const commits = useQuery({ queryKey: ["commits", issueKey], queryFn: () => api.commits(issueKey) });
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });
  const projectKeyOfIssue = issue.data?.project_key ?? "";
  const milestones = useQuery({
    queryKey: ["milestones", projectKeyOfIssue],
    queryFn: () => api.listMilestones(projectKeyOfIssue),
    enabled: !!projectKeyOfIssue,
  });
  const releases = useQuery({
    queryKey: ["releases", projectKeyOfIssue],
    queryFn: () => api.listReleases(projectKeyOfIssue),
    enabled: !!projectKeyOfIssue,
  });

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
  const archive = useMutation({
    mutationFn: (archived: boolean) => (archived ? api.archiveIssue(issueKey) : api.unarchiveIssue(issueKey)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issueKey] });
      qc.invalidateQueries({ queryKey: ["issues"] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    },
  });
  // Opening the issue is the act the badge was pointing at, so it counts as reading.
  // A count that survives opening the thing it referred to is a count people learn to
  // ignore. Fire-and-forget: a failure here must never block the page.
  useEffect(() => {
    api
      .markIssueRead(issueKey)
      .then(() => qc.invalidateQueries({ queryKey: ["notifications"] }))
      .catch(() => undefined);
  }, [issueKey, qc]);

  const snooze = useMutation({
    mutationFn: (until: string | null) =>
      until ? api.snoozeIssue(issueKey, until, "") : api.wakeIssue(issueKey),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["issue", issueKey] });
      qc.invalidateQueries({ queryKey: ["issues"] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
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

  if (issue.isLoading) return <div className="px-4 md:px-9 py-10 text-sm text-graphite">Loading…</div>;
  if (issue.isError || !issue.data)
    return <div className="px-4 md:px-9 py-10 text-sm text-critical">{(issue.error as Error)?.message ?? "Not found"}</div>;

  const i = issue.data;
  const commentItems = comments.data?.pages.flatMap((p) => p.items) ?? [];
  const activityItems = activity.data?.pages.flatMap((p) => p.items) ?? [];
  // Every rail control writes through these mutations; without a visible error a
  // rejected write just snaps the control back to its old value with no explanation.
  const railError = (transition.error ?? patch.error ?? move.error ?? archive.error ?? snooze.error) as Error | null;

  return (
    <div>
      {/* Topbar */}
      <div className="sticky top-0 z-10 flex h-[60px] items-center justify-between gap-2 border-b border-hairline bg-paper/80 px-4 backdrop-blur md:px-8">
        <div className="flex min-w-0 items-center gap-2.5 text-sm">
          <Link to="/issues" className="shrink-0 text-graphite transition hover:text-ink">
            Issues
          </Link>
          {/* The key is already the page heading below; on a phone the breadcrumb is
              costing room the actions need. */}
          <span className="hidden text-graphite-soft sm:inline">›</span>
          <span className="hidden font-mono font-medium text-blueprint sm:inline">{i.key}</span>
        </div>
        <div className="relative flex shrink-0 items-center gap-2 md:gap-2.5">
          <WatchButton issueKey={issueKey} />
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
                    archive.mutate(!i.archived_at);
                  }}
                  disabled={archive.isPending}
                  className="block w-full px-3 py-2 text-left text-sm text-ink transition hover:bg-panel disabled:opacity-50"
                >
                  {i.archived_at ? "Unarchive issue" : "Archive issue"}
                </button>
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
              {i.archived_at && (
                <span className="rounded-sm border border-hairline bg-panel px-2.5 py-1 font-mono text-[10px] font-semibold uppercase tracking-caps text-graphite">
                  Archived
                </span>
              )}
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
            <div className="group">
              <Reactions issueKey={issueKey} />
            </div>
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

          <AttachmentsSection issueKey={issueKey} items={attachments.data?.items ?? []} />

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
            {comments.hasNextPage && (
              <button
                onClick={() => comments.fetchNextPage()}
                disabled={comments.isFetchingNextPage}
                className="self-start text-sm font-semibold text-blueprint transition hover:opacity-80 disabled:opacity-50"
              >
                {comments.isFetchingNextPage
                  ? "Loading…"
                  : `Load earlier comments (${(comments.data?.pages[0]?.total ?? 0) - commentItems.length} more)`}
              </button>
            )}
            {commentItems.map((c) => (
              <CommentCard
                key={c.id}
                comment={c}
                isAuthor={!!me.data && c.author?.id === me.data.id}
                canModerate={["owner", "admin", "maintainer"].includes(me.data?.role ?? "")}
                issueKey={issueKey}
              />
            ))}
            {addComment.isError && (
              <p className="text-sm text-critical">{(addComment.error as Error).message}</p>
            )}

            <div className="flex items-start gap-3">
              <Avatar user={i.reporter} size={28} />
              <MentionTextarea
                textareaRef={composerRef}
                issueKey={issueKey}
                value={comment}
                onChange={setComment}
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

          {railError && (
            <p className="rounded-md border border-critical-border bg-critical-soft px-3 py-2 text-sm text-critical">
              {railError.message}
            </p>
          )}

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

          <MetaRow label="Components">
            <ComponentsSelect
              projectKey={i.project_key}
              value={i.components ?? []}
              onChange={(components) => patch.mutate({ components })}
            />
          </MetaRow>

          <MetaRow label="Milestone">
            <MilestoneControl
              milestoneId={i.milestone_id ?? null}
              milestoneTitle={i.milestone}
              milestones={milestones.data?.items ?? []}
              onChange={(milestone_id) => patch.mutate({ milestone_id })}
            />
          </MetaRow>

          <MetaRow label="Release">
            <ReleaseControl
              releaseId={i.release_id ?? null}
              releaseVersion={i.release}
              releases={releases.data?.items ?? []}
              onChange={(release_id) => patch.mutate({ release_id })}
            />
          </MetaRow>

          <div className="flex gap-4">
            <MetaRow label="Priority" className="grow">
              <PriorityControl priority={i.priority} onChange={(priority) => patch.mutate({ priority })} />
            </MetaRow>
            <MetaRow label="Severity" className="grow">
              <SeverityMark severity={i.severity} />
            </MetaRow>
          </div>

          <MetaRow label="Due">
            <DueControl
              dueAt={i.due_at ?? null}
              resolved={!!i.resolved_at}
              sla={i.sla}
              onChange={(due_at) => patch.mutate({ due_at })}
            />
          </MetaRow>

          {(i.version_fixed || i.version_affected) && (
            <MetaRow label="Version">
              <div className="flex items-center gap-2 text-sm font-medium text-ink">
                <IconMilestone size={15} className="text-graphite" />
                {i.version_fixed ? `${i.version_fixed} — fixed` : `${i.version_affected} — affected`}
              </div>
            </MetaRow>
          )}

          <SnoozeControl issue={i} onSnooze={(until) => snooze.mutate(until)} pending={snooze.isPending} />
      <LinkedIssues issueKey={issueKey} />
      <ReferencedBy issueKey={issueKey} />

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

          {activityItems.length > 0 && (
            <div className="flex flex-col gap-2.5 border-t border-hairline pt-5">
              <MicroLabel>Activity</MicroLabel>
              <ul className="flex flex-col gap-2">
                {activityItems.map((a) => (
                  <li key={a.id} className="text-xs text-graphite">
                    <span className="font-medium text-ink">{a.actor?.display_name ?? "system"}</span>{" "}
                    {describeActivity(a)}
                    <span className="text-graphite-soft"> · {timeAgo(a.occurred_at)}</span>
                  </li>
                ))}
              </ul>
              {activity.hasNextPage && (
                <button
                  onClick={() => activity.fetchNextPage()}
                  disabled={activity.isFetchingNextPage}
                  className="self-start text-xs font-semibold text-blueprint transition hover:opacity-80 disabled:opacity-50"
                >
                  {activity.isFetchingNextPage ? "Loading…" : "Show older activity"}
                </button>
              )}
            </div>
          )}
          </div>
        </aside>
      </div>

      {editing && <EditIssueForm issue={i} onClose={() => setEditing(false)} />}
    </div>
  );
}

function WatchButton({ issueKey }: { issueKey: string }) {
  const qc = useQueryClient();
  const watchers = useQuery({ queryKey: ["watchers", issueKey], queryFn: () => api.listWatchers(issueKey) });
  const watching = watchers.data?.watching ?? false;
  const count = watchers.data?.items.length ?? 0;

  const toggle = useMutation({
    mutationFn: () => (watching ? api.unwatchIssue(issueKey) : api.watchIssue(issueKey)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["watchers", issueKey] }),
  });

  return (
    <button
      onClick={() => toggle.mutate()}
      disabled={toggle.isPending || watchers.isLoading}
      title={watching ? "Stop watching this issue" : "Get notified about changes to this issue"}
      className={`flex h-[34px] items-center gap-1.5 rounded-md border px-3.5 text-sm font-semibold transition disabled:opacity-60 ${
        watching
          ? "border-blueprint bg-blueprint-soft text-blueprint"
          : "border-hairline text-ink hover:border-graphite"
      }`}
    >
      <IconEye size={15} className={watching ? "text-blueprint" : "text-graphite"} />
      {watching ? "Watching" : "Watch"}
      {count > 0 && <span className="font-mono text-xs">{count}</span>}
    </button>
  );
}

// Human labels per direction: an incoming "blocks" edge means the other issue
// blocks this one, so it reads "blocked by", and so on.
const RELATION_LABELS: Record<RelationKind, { out: string; in: string }> = {
  blocks: { out: "blocks", in: "blocked by" },
  blocked_by: { out: "blocked by", in: "blocks" },
  duplicates: { out: "duplicates", in: "duplicated by" },
  relates: { out: "relates to", in: "relates to" },
  caused_by: { out: "caused by", in: "causes" },
};
const RELATION_KINDS: RelationKind[] = ["blocks", "blocked_by", "duplicates", "relates", "caused_by"];

function LinkedIssues({ issueKey }: { issueKey: string }) {
  const qc = useQueryClient();
  const relations = useQuery({ queryKey: ["relations", issueKey], queryFn: () => api.listRelations(issueKey) });
  const [kind, setKind] = useState<RelationKind>("blocks");
  const [otherKey, setOtherKey] = useState("");

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["relations", issueKey] });
    qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    qc.invalidateQueries({ queryKey: ["issues"] }); // open_blockers on cards
  };
  const add = useMutation({
    mutationFn: () => api.addRelation(issueKey, kind, otherKey.trim()),
    onSuccess: () => {
      setOtherKey("");
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteRelation(id), onSuccess: invalidate });

  const items = relations.data?.items ?? [];

  return (
    <div className="flex flex-col gap-2.5 border-t border-hairline pt-5">
      <MicroLabel>Linked issues</MicroLabel>
      {items.map((rel) => (
        <div key={`${rel.id}-${rel.direction}`} className="group flex items-center gap-2 text-sm">
          <span className="shrink-0 text-xs text-graphite">{RELATION_LABELS[rel.kind][rel.direction]}</span>
          <Link to={`/issues/${rel.issue_key}`} className="shrink-0 font-mono text-xs font-medium text-blueprint hover:underline">
            {rel.issue_key}
          </Link>
          <span className="truncate text-xs text-graphite-soft" title={rel.title}>
            {rel.title}
          </span>
          <span className="grow" />
          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${statusTone[rel.status].dot}`} title={statusLabel[rel.status]} />
          <button
            onClick={() => del.mutate(rel.id)}
            aria-label="Remove link"
            className="shrink-0 text-xs text-graphite-soft opacity-0 transition hover:text-critical group-hover:opacity-100"
          >
            ✕
          </button>
        </div>
      ))}
      <div className="flex items-center gap-1.5">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as RelationKind)}
          aria-label="Relation kind"
          className="shrink-0 rounded-md border border-hairline bg-paper px-1.5 py-1 text-xs text-ink outline-none focus:border-blueprint"
        >
          {RELATION_KINDS.map((k) => (
            <option key={k} value={k}>
              {RELATION_LABELS[k].out}
            </option>
          ))}
        </select>
        <input
          value={otherKey}
          onChange={(e) => setOtherKey(e.target.value.toUpperCase())}
          onKeyDown={(e) => {
            if (e.key === "Enter" && otherKey.trim()) add.mutate();
          }}
          placeholder="BUG-42"
          className="w-20 grow rounded-md border border-hairline bg-paper px-2 py-1 font-mono text-xs text-ink outline-none placeholder:text-graphite-soft focus:border-blueprint"
        />
        <button
          disabled={!otherKey.trim() || add.isPending}
          onClick={() => add.mutate()}
          className="shrink-0 rounded-md border border-hairline px-2 py-1 text-xs font-medium text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50"
        >
          Link
        </button>
      </div>
      {add.isError && <p className="text-xs text-critical">{(add.error as Error).message}</p>}
    </div>
  );
}

/**
 * useAttachInsert uploads images pasted or dropped into a text field and writes the
 * markdown reference at the caret.
 *
 * Screenshots are the highest-information part of most bug reports and were the most
 * annoying thing to include: the attachment panel took a drop, but a screenshot in the
 * clipboard had to be saved to a file first, and the result was a filename in a list
 * rather than a picture in the text.
 *
 * A placeholder goes in immediately and is replaced by key, not by offset — so typing
 * during a slow upload cannot land the URL in the middle of a word, and a failed upload
 * leaves a visible comment rather than silently eating the paragraph.
 */
function useAttachInsert(
  issueKey: string | undefined,
  value: string,
  onChange: (v: string) => void,
  textareaRef: React.RefObject<HTMLTextAreaElement>,
) {
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  // The latest text, so an upload that resolves later edits what is on screen now
  // rather than the snapshot it started from.
  const latest = useRef(value);
  latest.current = value;

  const upload = async (files: File[]) => {
    if (!issueKey) return;
    setError(null);
    for (const file of files) {
      const token = `![uploading ${file.name}…]()`;
      const el = textareaRef.current;
      const caret = el?.selectionStart ?? latest.current.length;
      const withToken = `${latest.current.slice(0, caret)}${token}${latest.current.slice(caret)}`;
      latest.current = withToken;
      onChange(withToken);

      try {
        const a = await api.uploadAttachment(issueKey, file);
        latest.current = latest.current.replace(token, `![${a.filename}](${api.attachmentSrc(a.id)})`);
        qc.invalidateQueries({ queryKey: ["attachments", issueKey] });
      } catch (e) {
        latest.current = latest.current.replace(token, `<!-- upload failed: ${file.name} -->`);
        setError((e as Error).message);
      }
      onChange(latest.current);
    }
  };

  const imagesFrom = (list: FileList | null | undefined, items?: DataTransferItemList) => {
    if (items) {
      return Array.from(items)
        .filter((i) => i.kind === "file" && i.type.startsWith("image/"))
        .map((i) => i.getAsFile())
        .filter((f): f is File => f !== null);
    }
    return Array.from(list ?? []).filter((f) => f.type.startsWith("image/"));
  };

  return {
    error,
    onPaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      if (!issueKey) return;
      const images = imagesFrom(null, e.clipboardData?.items);
      if (images.length === 0) return; // plain text paste — leave it alone
      e.preventDefault();
      void upload(images);
    },
    onDrop: (e: DragEvent) => {
      if (!issueKey) return;
      const images = imagesFrom(e.dataTransfer?.files);
      if (images.length === 0) return;
      e.preventDefault();
      void upload(images);
    },
    onDragOver: (e: DragEvent) => {
      // Without this the browser navigates to the dropped file instead of letting the
      // drop handler run.
      if (issueKey) e.preventDefault();
    },
  };
}

/**
 * MentionTextarea is the comment composer with @-autocomplete.
 *
 * Keyboard-first: ↑/↓ move, Enter or Tab accept, Escape dismisses. Those keys only get
 * intercepted while the picker is open, so Enter still means newline the rest of the
 * time — the alternative is a composer that eats your paragraph breaks.
 */
function MentionTextarea({
  textareaRef,
  value,
  onChange,
  placeholder,
  rows,
  className,
  issueKey,
}: {
  textareaRef: React.RefObject<HTMLTextAreaElement>;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  rows?: number;
  className?: string;
  /** When set, pasted or dropped images upload to this issue and insert as markdown. */
  issueKey?: string;
}) {
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const attach = useAttachInsert(issueKey, value, onChange, textareaRef);
  const [mention, setMention] = useState<{ query: string; start: number } | null>(null);
  const [active, setActive] = useState(0);

  const candidates = mention ? matchUsers(users.data?.items ?? [], mention.query) : [];
  const open = mention !== null && candidates.length > 0;

  const refresh = (el: HTMLTextAreaElement) => {
    setMention(mentionQuery(el.value, el.selectionStart ?? el.value.length));
    setActive(0);
  };

  const accept = (index: number) => {
    const user = candidates[index];
    if (!user || !mention) return;
    const el = textareaRef.current;
    const caret = el?.selectionStart ?? value.length;
    const handle = preferredHandle(user);
    const next = `${value.slice(0, mention.start)}@${handle} ${value.slice(caret)}`;
    onChange(next);
    setMention(null);
    // Put the caret after the inserted handle rather than at the end of the body —
    // mentioning someone mid-sentence must not jump you to the bottom of it.
    const at = mention.start + handle.length + 2;
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(at, at);
    });
  };

  return (
    <div className="relative grow">
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          refresh(e.currentTarget);
          const t = e.currentTarget;
          t.style.height = "auto";
          t.style.height = `${Math.min(t.scrollHeight, 176)}px`; // grow with content, cap at ~176px
        }}
        onClick={(e) => refresh(e.currentTarget)}
        onKeyUp={(e) => {
          if (e.key === "ArrowLeft" || e.key === "ArrowRight") refresh(e.currentTarget);
        }}
        onKeyDown={(e) => {
          if (!open) return;
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((i) => (i + 1) % candidates.length);
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((i) => (i - 1 + candidates.length) % candidates.length);
          } else if (e.key === "Enter" || e.key === "Tab") {
            e.preventDefault();
            accept(active);
          } else if (e.key === "Escape") {
            setMention(null);
          }
        }}
        onBlur={() => setMention(null)}
        onPaste={attach.onPaste}
        onDrop={attach.onDrop}
        onDragOver={attach.onDragOver}
        placeholder={placeholder}
        rows={rows}
        className={`w-full ${className ?? ""}`}
      />
      {attach.error && <p className="mt-1 text-xs text-critical">{attach.error}</p>}
      {open && (
        <ul className="absolute bottom-full left-0 z-20 mb-1 w-64 overflow-hidden rounded-md border border-hairline bg-paper shadow-lg">
          {candidates.map((u, index) => (
            <li key={u.id}>
              <button
                type="button"
                // onMouseDown, not onClick: blur fires first and would close the list
                // before the click ever lands.
                onMouseDown={(e) => {
                  e.preventDefault();
                  accept(index);
                }}
                onMouseEnter={() => setActive(index)}
                className={`flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm ${
                  index === active ? "bg-blueprint-soft text-ink" : "text-graphite"
                }`}
              >
                <Avatar user={u} size={18} />
                <span className="truncate">{u.display_name}</span>
                <span className="truncate font-mono text-[11px] text-graphite-soft">@{preferredHandle(u)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// Relative offsets for the common cases. An exact date is the input below them.
const SNOOZE_PRESETS: { label: string; hours: number }[] = [
  { label: "Tomorrow", hours: 24 },
  { label: "Next week", hours: 24 * 7 },
  { label: "In a month", hours: 24 * 30 },
];

/**
 * SnoozeControl hides an issue until a date. Shown in the rail rather than buried in the
 * ⋯ menu next to Archive: those two look alike and mean very different things — archive
 * is indefinite and manual to reverse, a snooze comes back on its own.
 */
function SnoozeControl({
  issue,
  onSnooze,
  pending,
}: {
  issue: Issue;
  onSnooze: (until: string | null) => void;
  pending: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [date, setDate] = useState("");
  const snoozed = issue.snoozed_until ? new Date(issue.snoozed_until) : null;
  const active = snoozed !== null && snoozed.getTime() > Date.now();

  if (active) {
    return (
      <div className="flex flex-col gap-2 border-t border-hairline pt-5">
        <MicroLabel>Snoozed</MicroLabel>
        <p className="text-sm text-ink">
          Hidden until {snoozed.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}
        </p>
        {issue.snooze_note && <p className="text-xs text-graphite-soft">{issue.snooze_note}</p>}
        <button
          onClick={() => onSnooze(null)}
          disabled={pending}
          className="self-start text-xs font-semibold text-blueprint transition hover:opacity-80 disabled:opacity-50"
        >
          {pending ? "Waking…" : "Wake now"}
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2 border-t border-hairline pt-5">
      <div className="flex items-center justify-between">
        <MicroLabel>Snooze</MicroLabel>
        <button
          onClick={() => setOpen((o) => !o)}
          className="text-xs font-semibold text-blueprint transition hover:opacity-80"
        >
          {open ? "Cancel" : "Snooze…"}
        </button>
      </div>
      {open && (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap gap-1.5">
            {SNOOZE_PRESETS.map((preset) => (
              <button
                key={preset.label}
                disabled={pending}
                onClick={() => {
                  onSnooze(new Date(Date.now() + preset.hours * 3600_000).toISOString());
                  setOpen(false);
                }}
                className="rounded-full border border-hairline px-2.5 py-1 text-xs text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50"
              >
                {preset.label}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-1.5">
            <input
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              className="grow rounded-md border border-hairline bg-paper px-2 py-1 text-xs text-ink outline-none focus:border-blueprint"
            />
            <button
              disabled={!date || pending}
              onClick={() => {
                // Local 09:00 on the chosen day: "until the 14th" means the start of
                // that working day, not midnight the night before.
                const at = new Date(`${date}T09:00`);
                onSnooze(at.toISOString());
                setOpen(false);
                setDate("");
              }}
              className="shrink-0 rounded-md border border-hairline px-2 py-1 text-xs font-medium text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50"
            >
              Set
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// ReferencedBy lists issues whose prose mentions this one. Read-only by design: a
// reference is derived from the text, so the way to remove one is to edit the sentence
// that made it, not to click an ✕ here and have it reappear on the next save.
function ReferencedBy({ issueKey }: { issueKey: string }) {
  const refs = useQuery({ queryKey: ["references", issueKey], queryFn: () => api.listReferences(issueKey) });
  const items = refs.data?.items ?? [];
  if (items.length === 0) return null;

  return (
    <div className="flex flex-col gap-2.5 border-t border-hairline pt-5">
      <MicroLabel>Referenced by</MicroLabel>
      {items.map((ref) => (
        <div key={ref.issue_id} className="flex items-center gap-2 text-sm">
          <Link to={`/issues/${ref.issue_key}`} className="shrink-0 font-mono text-xs font-medium text-blueprint hover:underline">
            {ref.issue_key}
          </Link>
          <span className="truncate text-xs text-graphite-soft" title={ref.title}>
            {ref.title}
          </span>
          <span className="grow" />
          {ref.in_comment && <span className="shrink-0 text-[10px] uppercase tracking-caps text-graphite-soft">comment</span>}
          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${statusTone[ref.status].dot}`} title={statusLabel[ref.status]} />
        </div>
      ))}
    </div>
  );
}

/**
 * Reactions is the row of emoji under a body. Acknowledgement is most of the traffic on
 * any issue and the only way to express it used to be a comment saying "+1", which
 * notified every watcher — so these deliberately notify nobody and write no activity.
 */
function Reactions({ issueKey, commentId }: { issueKey: string; commentId?: string }) {
  const qc = useQueryClient();
  const [picking, setPicking] = useState(false);
  const all = useQuery({ queryKey: ["reactions", issueKey], queryFn: () => api.listReactions(issueKey) });
  const toggle = useMutation({
    mutationFn: (emoji: string) => api.toggleReaction(issueKey, emoji, commentId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["reactions", issueKey] }),
  });

  // One query serves every body on the page; each row filters to its own target.
  const mine = (all.data?.items ?? []).filter((r) => (r.comment_id ?? undefined) === commentId);
  const available = all.data?.emoji ?? [];

  return (
    <div className="relative flex flex-wrap items-center gap-1.5">
      {mine.map((r) => (
        <button
          key={r.emoji}
          onClick={() => toggle.mutate(r.emoji)}
          title={r.users.join(", ")}
          className={`flex h-6 items-center gap-1 rounded-full border px-2 text-xs transition ${
            r.mine
              ? "border-blueprint bg-blueprint-soft font-semibold text-blueprint"
              : "border-hairline text-graphite hover:border-graphite"
          }`}
        >
          <span>{EMOJI[r.emoji] ?? r.emoji}</span>
          <span className="font-mono">{r.count}</span>
        </button>
      ))}
      <button
        onClick={() => setPicking((o) => !o)}
        aria-label="Add a reaction"
        className="flex h-6 items-center rounded-full border border-hairline px-2 text-xs text-graphite-soft opacity-0 transition hover:border-graphite hover:text-graphite group-hover:opacity-100"
      >
        ☺+
      </button>
      {picking && (
        <>
          <button className="fixed inset-0 z-10 cursor-default" aria-hidden onClick={() => setPicking(false)} />
          <div className="absolute bottom-7 left-0 z-20 flex gap-1 rounded-md border border-hairline bg-paper p-1.5 shadow-lg">
            {available.map((e) => (
              <button
                key={e}
                onClick={() => {
                  setPicking(false);
                  toggle.mutate(e);
                }}
                title={e}
                className="grid h-7 w-7 place-items-center rounded transition hover:bg-panel"
              >
                {EMOJI[e] ?? e}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function CommentCard({
  comment: c,
  isAuthor,
  canModerate,
  issueKey,
}: {
  comment: Comment;
  isAuthor: boolean;
  canModerate: boolean;
  issueKey: string;
}) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(c.body_md);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["comments", issueKey] });
    qc.invalidateQueries({ queryKey: ["activity", issueKey] });
  };
  const save = useMutation({
    mutationFn: () => api.updateComment(c.id, draft),
    onSuccess: () => {
      setEditing(false);
      invalidate();
    },
  });
  const del = useMutation({ mutationFn: () => api.deleteComment(c.id), onSuccess: invalidate });

  return (
    <div className="group flex gap-3">
      <Avatar user={c.author} size={28} />
      <div className="flex min-w-0 grow flex-col gap-1 rounded-md border border-hairline bg-paper p-3.5">
        <div className="flex items-center gap-2 text-xs text-graphite-soft">
          <span className="font-medium text-ink">{c.author?.display_name ?? "unknown"}</span>·
          <span>{timeAgo(c.created_at)}</span>
          {c.edited_at && <span className="italic">(edited)</span>}
          <span className="grow" />
          {isAuthor && !editing && (
            <button
              onClick={() => {
                setDraft(c.body_md);
                setEditing(true);
              }}
              className="opacity-0 transition hover:text-ink group-hover:opacity-100"
            >
              Edit
            </button>
          )}
          {(isAuthor || canModerate) && !editing && (
            <button
              onClick={() => {
                if (window.confirm("Delete this comment?")) del.mutate();
              }}
              className="opacity-0 transition hover:text-critical group-hover:opacity-100"
            >
              Delete
            </button>
          )}
        </div>
        {editing ? (
          <div className="flex flex-col gap-2">
            <textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              rows={3}
              autoFocus
              className="w-full resize-y rounded-md border border-hairline bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-blueprint"
            />
            {save.isError && <p className="text-sm text-critical">{(save.error as Error).message}</p>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setEditing(false)} className="px-3 py-1.5 text-sm text-graphite hover:text-ink">
                Cancel
              </button>
              <button
                disabled={!draft.trim() || save.isPending}
                onClick={() => save.mutate()}
                className="rounded-md bg-blueprint px-3 py-1.5 text-sm font-semibold text-paper transition hover:opacity-90 disabled:opacity-50"
              >
                {save.isPending ? "Saving…" : "Save"}
              </button>
            </div>
          </div>
        ) : (
          <>
            <Markdown body={c.body_md} />
            <div className="mt-2">
              <Reactions issueKey={issueKey} commentId={c.id} />
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function AttachmentsSection({ issueKey, items }: { issueKey: string; items: import("../../lib/api").Attachment[] }) {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadAttachment(issueKey, file),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["attachments", issueKey] });
      qc.invalidateQueries({ queryKey: ["activity", issueKey] });
    },
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteAttachment(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["attachments", issueKey] }),
  });

  const onFiles = (files: FileList | null) => {
    if (!files) return;
    for (const f of Array.from(files)) upload.mutate(f);
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    onFiles(e.dataTransfer.files);
  };

  return (
    <section
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
      className={`flex flex-col gap-3 rounded-md border border-dashed p-4 transition ${
        dragging ? "border-blueprint bg-blueprint-soft/40" : "border-hairline"
      }`}
    >
      <div className="flex items-center justify-between">
        <MicroLabel>Attachments</MicroLabel>
        <button
          onClick={() => fileRef.current?.click()}
          disabled={upload.isPending}
          className="rounded-md border border-hairline px-3 py-1.5 text-sm font-medium text-graphite transition hover:border-graphite hover:text-ink disabled:opacity-50"
        >
          {upload.isPending ? "Uploading…" : "Add file"}
        </button>
        <input ref={fileRef} type="file" multiple hidden onChange={(e) => onFiles(e.target.files)} />
      </div>
      {upload.isError && <p className="text-sm text-critical">{(upload.error as Error).message}</p>}
      {items.length === 0 && !upload.isPending && (
        <p className="text-xs text-graphite-soft">No files yet — drop them here or use “Add file”.</p>
      )}
      {items.map((a) => (
        <div key={a.id} className="flex items-center gap-3">
          {a.content_type.startsWith("image/") && (
            <a href={api.attachmentSrc(a.id)} target="_blank" rel="noreferrer noopener" className="shrink-0">
              <img
                src={api.attachmentSrc(a.id)}
                alt={a.filename}
                className="h-9 w-9 rounded border border-hairline object-cover"
              />
            </a>
          )}
          <button
            onClick={() => api.downloadAttachment(a.id, a.filename)}
            className="truncate font-mono text-sm font-medium text-blueprint hover:underline"
            title="Download"
          >
            {a.filename}
          </button>
          <span className="shrink-0 font-mono text-xs text-graphite-soft">
            {formatBytes(a.size_bytes)} · {a.uploader?.display_name ?? "unknown"} · {timeAgo(a.created_at)}
          </span>
          <span className="grow" />
          <button
            onClick={() => {
              if (window.confirm(`Delete ${a.filename}?`)) del.mutate(a.id);
            }}
            className="shrink-0 text-xs text-graphite-soft transition hover:text-critical"
            aria-label={`Delete ${a.filename}`}
          >
            ✕
          </button>
        </div>
      ))}
    </section>
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

// MARKDOWN_PLUGINS and MarkdownLink are shared by every body on the page: descriptions,
// bug narrative callouts and comments. Centralised so a new call site cannot quietly opt
// out of issue-key linkification.
const MARKDOWN_PLUGINS = [remarkGfm, remarkIssueKeys, remarkMentions];

/** Keeps in-app links inside the SPA; anything external opens safely in a new tab. */
function MarkdownLink({ href, children }: { href?: string; children?: ReactNode }) {
  if (href?.startsWith("/")) {
    return (
      <Link to={href} className="text-blueprint hover:underline">
        {children}
      </Link>
    );
  }
  return (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  );
}

/** Inline images are capped and open full size in a new tab; a screenshot should
 *  illustrate the paragraph, not push it off the screen. */
function MarkdownImage({ src, alt }: { src?: string; alt?: string }) {
  if (!src) return null;
  return (
    <a href={src} target="_blank" rel="noreferrer noopener">
      <img src={src} alt={alt ?? ""} className="my-2 max-h-96 max-w-full rounded-md border border-hairline" />
    </a>
  );
}

function Markdown({ body, className = "markdown" }: { body?: string; className?: string }) {
  return (
    <div className={className}>
      <ReactMarkdown remarkPlugins={MARKDOWN_PLUGINS} components={{ a: MarkdownLink, img: MarkdownImage }}>
        {body || "_No content_"}
      </ReactMarkdown>
    </div>
  );
}

function Callout({ tone, label, body }: { tone: "resolved" | "critical"; label: string; body: string }) {
  const border = tone === "resolved" ? "border-l-resolved" : "border-l-critical";
  const text = tone === "resolved" ? "text-resolved" : "text-critical";
  return (
    <div className={`flex grow basis-0 flex-col gap-2 rounded-md border-l-[3px] bg-panel/50 px-4 py-3.5 ${border}`}>
      <span className={`font-mono text-[10px] font-medium uppercase tracking-caps ${text}`}>{label}</span>
      <Markdown body={body} className="markdown text-[14px] leading-[1.55]" />
    </div>
  );
}

/**
 * DueControl edits the issue's own deadline and, underneath it, reports where the
 * issue stands against its project's SLA.
 *
 * The two are deliberately shown together but never conflated: the due date is a
 * promise somebody made about this issue, the SLA is what the project already
 * committed to for everything of this severity. Editing one must not look like it
 * moves the other.
 */
function DueControl({
  dueAt,
  resolved,
  sla,
  onChange,
}: {
  dueAt: string | null;
  resolved: boolean;
  sla?: IssueSLA;
  onChange: (dueAt: string) => void;
}) {
  // <input type="date"> speaks "YYYY-MM-DD" in local time; the API accepts exactly
  // that and resolves it to the end of that day, so no timezone maths happens here.
  const value = dueAt ? new Date(dueAt).toLocaleDateString("en-CA") : "";
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <input
          type="date"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-label="Due date"
          className="h-[30px] rounded-md border border-hairline bg-paper px-2 text-sm text-ink"
        />
        {dueAt && (
          <button
            onClick={() => onChange("")}
            className="text-xs font-semibold text-graphite transition hover:text-critical"
          >
            Clear
          </button>
        )}
      </div>
      {dueAt && (
        <div className="flex flex-wrap items-center gap-1.5">
          <DueChip dueAt={dueAt} resolved={resolved} />
        </div>
      )}
      {sla && (
        <div className="flex flex-col gap-1 border-t border-hairline pt-2">
          <div className="flex items-center gap-1.5">
            <span className="text-xs text-graphite">SLA</span>
            <SLAPill sla={sla} />
            {sla.state !== "breached" && sla.state !== "at_risk" && (
              <span className="text-xs font-medium text-resolved">on target</span>
            )}
          </div>
          <SLALine label="First response" due={sla.response_due} state={sla.response_state} />
          <SLALine label="Resolution" due={sla.resolution_due} state={sla.resolution_state} />
        </div>
      )}
    </div>
  );
}

function SLALine({ label, due, state }: { label: string; due?: string; state: string }) {
  if (!due) return null;
  const tone =
    state === "breached" ? "text-critical" : state === "at_risk" ? "text-high" : "text-graphite";
  return (
    <div className="flex items-baseline justify-between gap-2 text-xs">
      <span className="text-graphite-soft">{label}</span>
      <span className={`font-medium ${tone}`} title={new Date(due).toLocaleString()}>
        {state === "met" ? "met" : relativeDue(due)}
      </span>
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
  // Only offer transitions the workflow actually permits from here.
  const targets = [status, ...(ALLOWED_TRANSITIONS[status] ?? [])];
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
        {targets.map((s) => (
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

function MilestoneControl({
  milestoneId,
  milestoneTitle,
  milestones,
  onChange,
}: {
  milestoneId: string | null;
  milestoneTitle?: string;
  milestones: Milestone[];
  onChange: (milestoneId: string) => void;
}) {
  const open = milestones.filter((m) => m.state === "open" || m.id === milestoneId);
  return (
    <div className="relative">
      <div className="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 transition hover:bg-panel">
        <IconMilestone size={15} className="text-graphite" />
        <span className={`text-sm font-medium ${milestoneId ? "text-ink" : "text-graphite-soft"}`}>
          {milestoneId ? milestoneTitle || "Milestone" : "No milestone"}
        </span>
        <IconChevronDown size={12} className="text-graphite-soft" />
      </div>
      <select
        value={milestoneId ?? UNASSIGNED}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Change milestone"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        <option value={UNASSIGNED}>No milestone</option>
        {open.map((m) => (
          <option key={m.id} value={m.id}>
            {m.title}
            {m.state === "closed" ? " (closed)" : ""}
          </option>
        ))}
      </select>
    </div>
  );
}

function ReleaseControl({
  releaseId,
  releaseVersion,
  releases,
  onChange,
}: {
  releaseId: string | null;
  releaseVersion?: string;
  releases: Release[];
  onChange: (releaseId: string) => void;
}) {
  const options = releases.filter((r) => r.state === "draft" || r.id === releaseId);
  return (
    <div className="relative">
      <div className="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 transition hover:bg-panel">
        <span className={`font-mono text-sm font-medium ${releaseId ? "text-ink" : "text-graphite-soft"}`}>
          {releaseId ? releaseVersion || "Release" : "No release"}
        </span>
        <IconChevronDown size={12} className="text-graphite-soft" />
      </div>
      <select
        value={releaseId ?? UNASSIGNED}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Change release"
        className="absolute inset-0 cursor-pointer opacity-0"
      >
        <option value={UNASSIGNED}>No release</option>
        {options.map((r) => (
          <option key={r.id} value={r.id}>
            {r.version}
            {r.state === "published" ? " (published)" : ""}
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
