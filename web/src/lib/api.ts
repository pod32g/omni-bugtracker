// Minimal typed API client. After `npm run gen:api` you can swap this for openapi-fetch
// bound to `src/api/gen/schema.d.ts` for end-to-end type safety against the OpenAPI spec.

export type IssueType = "bug" | "task" | "feature" | "improvement";
export type IssueStatus =
  | "open" | "in_progress" | "blocked" | "ready_for_review"
  | "resolved" | "closed" | "reopened";
export type Severity = "critical" | "high" | "medium" | "low";
export type Priority = "p0" | "p1" | "p2" | "p3";

export interface User {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  role?: string;
}

export interface Project {
  id: string;
  key: string;
  name: string;
  description_md: string;
  default_assignee_id?: string | null;
  is_archived: boolean;
  created_at: string;
  my_role?: string; // caller's effective role (global, elevated by membership)
}

export interface ProjectMember {
  user: User;
  role: string;
  created_at: string;
}

export interface ApiToken {
  id: string;
  name: string;
  scopes: string[];
  last_used_at?: string;
  expires_at?: string;
  created_at: string;
}

export interface Label {
  id: string;
  name: string;
  color: string;
}

export interface Component {
  id: string;
  name: string;
  description_md: string;
  lead_id?: string | null;
  open_issues: number;
  created_at: string;
}

export interface Milestone {
  id: string;
  title: string;
  description_md: string;
  due_on?: string | null;
  state: "open" | "closed";
  open_issues: number;
  closed_issues: number;
  created_at: string;
}

export interface Release {
  id: string;
  version: string;
  name: string;
  notes_md: string;
  state: "draft" | "published";
  git_tag: string;
  released_at?: string | null;
  open_issues: number;
  done_issues: number;
  created_at: string;
}

export interface NewIssue {
  type: IssueType;
  title: string;
  description_md?: string;
  severity?: Severity;
  priority: Priority;
  assignee_id?: string;
  labels?: string[];
  components?: string[];
  milestone_id?: string; // PATCH only; zero UUID clears
  release_id?: string; // PATCH only; zero UUID clears
  repro_steps_md?: string;
  expected_md?: string;
  actual_md?: string;
  environment_md?: string;
}

// Sentinel assignee_id meaning "clear the assignee" on PATCH.
export const UNASSIGNED = "00000000-0000-0000-0000-000000000000";

export interface Issue {
  id: string;
  key: string;
  project_key: string;
  number: number;
  type: IssueType;
  title: string;
  description_md: string;
  status: IssueStatus;
  severity?: Severity;
  priority: Priority;
  assignee?: User;
  reporter?: User;
  labels: string[];
  components: string[];
  milestone_id?: string | null;
  milestone?: string;
  release_id?: string | null;
  release?: string;
  version_affected?: string;
  version_fixed?: string;
  repro_steps_md?: string;
  expected_md?: string;
  actual_md?: string;
  environment_md?: string;
  created_at: string;
  updated_at: string;
}

export interface Comment {
  id: string;
  author?: User;
  body_md: string;
  edited_at?: string | null;
  created_at: string;
}

export interface LinkedCommit {
  sha: string;
  repo: string;
  author: string;
  message: string;
  url: string;
  verb: string;
  created_at: string;
}

export interface Activity {
  id: string;
  actor?: User;
  verb: string;
  entity_type: string;
  issue_key?: string;
  occurred_at: string;
}

export interface Attachment {
  id: string;
  issue_id?: string;
  uploader?: User;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
}

export interface SearchHit {
  issue_key: string;
  project_key: string;
  title: string;
  status: IssueStatus;
  type: IssueType;
  snippet: string; // plain text with «…» match marks
  rank: number;
  matched_in: "issue" | "comment";
}

export interface DashboardOverview {
  open_issues: number;
  critical_issues: number;
  avg_resolution_hours: number;
  mttr_hours: number;
  regression_rate: number;
  issues_by_status: Record<string, number>;
  issues_by_component: Record<string, number>;
  team_workload: Record<string, number>;
  recent_activity: Activity[];
}

const BASE = "/api/v1";

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

// Browser auth is a first-party httpOnly session cookie set by /auth/callback, so no
// token handling is needed here. An optional localStorage token still works for
// programmatic/API-token use.
function optionalToken(): Record<string, string> {
  const t = localStorage.getItem("obt_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
}

// Deduped silent refresh: the 15-minute access token is renewed from the httpOnly
// refresh cookie so the user stays signed in without re-authenticating.
let refreshInFlight: Promise<boolean> | null = null;
function tryRefresh(): Promise<boolean> {
  if (!refreshInFlight) {
    refreshInFlight = fetch("/auth/refresh", { method: "POST", credentials: "include" })
      .then((r) => r.ok)
      .catch(() => false)
      .finally(() => {
        refreshInFlight = null;
      });
  }
  return refreshInFlight;
}

async function request<T>(path: string, init: RequestInit = {}, allowRefresh = true): Promise<T> {
  const doFetch = () =>
    fetch(BASE + path, {
      ...init,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...optionalToken(),
        ...(init.headers ?? {}),
      },
    });

  let res = await doFetch();
  if (res.status === 401 && allowRefresh && (await tryRefresh())) {
    res = await doFetch();
  }
  if (!res.ok) {
    const problem = await res.json().catch(() => ({ title: res.statusText }));
    throw new ApiError(res.status, problem.detail || problem.title || `HTTP ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

// Browser login helpers — navigate to the BFF endpoints (server-side OIDC exchange).
export const session = {
  login: () => window.location.assign("/auth/login"),
  logout: () => window.location.assign("/auth/logout"),
};

export const api = {
  me: () => request<User>("/me"),
  listUsers: () => request<{ items: User[] }>("/users"),
  updateUserRole: (id: string, role: string) =>
    request<User>(`/users/${id}/role`, { method: "PATCH", body: JSON.stringify({ role }) }),
  dashboard: () => request<DashboardOverview>("/dashboards/overview"),
  search: (q: string, limit = 20) =>
    request<{ items: SearchHit[]; total: number; source: string }>(
      `/search?q=${encodeURIComponent(q)}&limit=${limit}`,
    ),
  listProjects: () => request<{ items: Project[] }>("/projects"),
  createProject: (body: { key: string; name: string; description_md?: string }) =>
    request<Project>("/projects", { method: "POST", body: JSON.stringify(body) }),
  getProject: (key: string) => request<Project>(`/projects/${key}`),
  updateProject: (
    key: string,
    patch: { name?: string; description_md?: string; is_archived?: boolean; default_assignee_id?: string },
  ) => request<Project>(`/projects/${key}`, { method: "PATCH", body: JSON.stringify(patch) }),
  archiveProject: (key: string) => request<void>(`/projects/${key}`, { method: "DELETE" }),
  listTokens: () => request<{ items: ApiToken[] }>("/me/tokens"),
  createToken: (name: string, scopes: string[] = []) =>
    request<ApiToken & { token: string }>("/me/tokens", { method: "POST", body: JSON.stringify({ name, scopes }) }),
  revokeToken: (id: string) => request<void>(`/me/tokens/${id}`, { method: "DELETE" }),
  listLabels: (projectKey: string) => request<{ items: Label[] }>(`/projects/${projectKey}/labels`),
  listComponents: (projectKey: string) => request<{ items: Component[] }>(`/projects/${projectKey}/components`),
  createComponent: (projectKey: string, body: { name: string; description_md?: string; lead_id?: string }) =>
    request<Component>(`/projects/${projectKey}/components`, { method: "POST", body: JSON.stringify(body) }),
  updateComponent: (id: string, patch: { name?: string; description_md?: string; lead_id?: string }) =>
    request<Component>(`/components/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteComponent: (id: string) => request<void>(`/components/${id}`, { method: "DELETE" }),
  listMilestones: (projectKey: string) => request<{ items: Milestone[] }>(`/projects/${projectKey}/milestones`),
  createMilestone: (projectKey: string, body: { title: string; description_md?: string; due_on?: string }) =>
    request<Milestone>(`/projects/${projectKey}/milestones`, { method: "POST", body: JSON.stringify(body) }),
  updateMilestone: (
    id: string,
    patch: { title?: string; description_md?: string; due_on?: string; state?: "open" | "closed" },
  ) => request<Milestone>(`/milestones/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteMilestone: (id: string) => request<void>(`/milestones/${id}`, { method: "DELETE" }),
  listReleases: (projectKey: string) => request<{ items: Release[] }>(`/projects/${projectKey}/releases`),
  createRelease: (projectKey: string, body: { version: string; name?: string; notes_md?: string; git_tag?: string }) =>
    request<Release>(`/projects/${projectKey}/releases`, { method: "POST", body: JSON.stringify(body) }),
  updateRelease: (
    id: string,
    patch: { version?: string; name?: string; notes_md?: string; git_tag?: string; state?: "draft" | "published" },
  ) => request<Release>(`/releases/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteRelease: (id: string) => request<void>(`/releases/${id}`, { method: "DELETE" }),
  listProjectMembers: (projectKey: string) =>
    request<{ items: ProjectMember[] }>(`/projects/${projectKey}/members`),
  putProjectMember: (projectKey: string, userId: string, role: string) =>
    request<ProjectMember>(`/projects/${projectKey}/members/${userId}`, {
      method: "PUT",
      body: JSON.stringify({ role }),
    }),
  removeProjectMember: (projectKey: string, userId: string) =>
    request<void>(`/projects/${projectKey}/members/${userId}`, { method: "DELETE" }),
  listIssues: (projectKey: string, filter = "", sort = "") =>
    request<{ items: Issue[]; total: number }>(
      `/projects/${projectKey}/issues?filter=${encodeURIComponent(filter)}&sort=${encodeURIComponent(sort)}`,
    ),
  getIssue: (issueKey: string) => request<Issue>(`/issues/${issueKey}`),
  createIssue: (projectKey: string, body: NewIssue) =>
    request<Issue>(`/projects/${projectKey}/issues`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateIssue: (issueKey: string, patch: Partial<NewIssue>) =>
    request<Issue>(`/issues/${issueKey}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteIssue: (issueKey: string) =>
    request<void>(`/issues/${issueKey}`, { method: "DELETE" }),
  transition: (issueKey: string, to: IssueStatus) =>
    request<Issue>(`/issues/${issueKey}/transition`, {
      method: "POST",
      body: JSON.stringify({ to }),
    }),
  // Re-homes the issue into another project. The response carries the issue's NEW key
  // (it's reallocated a number in the target project), so callers should navigate to it.
  moveIssue: (issueKey: string, targetProjectKey: string) =>
    request<Issue>(`/issues/${issueKey}/move`, {
      method: "POST",
      body: JSON.stringify({ target_project_key: targetProjectKey }),
    }),
  listComments: (issueKey: string) => request<Comment[]>(`/issues/${issueKey}/comments`),
  addComment: (issueKey: string, body_md: string) =>
    request<Comment>(`/issues/${issueKey}/comments`, {
      method: "POST",
      body: JSON.stringify({ body_md }),
    }),
  activity: (issueKey: string) => request<Activity[]>(`/issues/${issueKey}/activity`),
  commits: (issueKey: string) => request<LinkedCommit[]>(`/issues/${issueKey}/commits`),
  listAttachments: (issueKey: string) => request<{ items: Attachment[] }>(`/issues/${issueKey}/attachments`),
  // Multipart upload — bypasses request() because the browser must set the
  // multipart boundary itself; keeps the same silent-refresh-on-401 behavior.
  uploadAttachment: async (issueKey: string, file: File): Promise<Attachment> => {
    const doFetch = () => {
      const fd = new FormData();
      fd.append("file", file, file.name);
      return fetch(`${BASE}/issues/${issueKey}/attachments`, {
        method: "POST",
        credentials: "include",
        headers: { ...optionalToken() },
        body: fd,
      });
    };
    let res = await doFetch();
    if (res.status === 401 && (await tryRefresh())) res = await doFetch();
    if (!res.ok) {
      const problem = await res.json().catch(() => ({ title: res.statusText }));
      throw new ApiError(res.status, problem.detail || problem.title || `HTTP ${res.status}`);
    }
    return res.json() as Promise<Attachment>;
  },
  // Blob download so it works with both cookie and bearer-token auth.
  downloadAttachment: async (id: string, filename: string) => {
    const res = await fetch(`${BASE}/attachments/${id}`, {
      credentials: "include",
      headers: { ...optionalToken() },
    });
    if (!res.ok) throw new ApiError(res.status, "download failed");
    const url = URL.createObjectURL(await res.blob());
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  },
  deleteAttachment: (id: string) => request<void>(`/attachments/${id}`, { method: "DELETE" }),
};
