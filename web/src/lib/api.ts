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
  is_archived: boolean;
  created_at: string;
}

export interface NewIssue {
  type: IssueType;
  title: string;
  description_md?: string;
  severity?: Severity;
  priority: Priority;
  assignee_id?: string;
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

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(BASE + path, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...optionalToken(),
      ...(init.headers ?? {}),
    },
  });
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
  dashboard: () => request<DashboardOverview>("/dashboards/overview"),
  listProjects: () => request<{ items: Project[] }>("/projects"),
  createProject: (body: { key: string; name: string; description_md?: string }) =>
    request<Project>("/projects", { method: "POST", body: JSON.stringify(body) }),
  listIssues: (projectKey: string, filter = "") =>
    request<{ items: Issue[]; total: number }>(
      `/projects/${projectKey}/issues?filter=${encodeURIComponent(filter)}`,
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
  listComments: (issueKey: string) => request<Comment[]>(`/issues/${issueKey}/comments`),
  addComment: (issueKey: string, body_md: string) =>
    request<Comment>(`/issues/${issueKey}/comments`, {
      method: "POST",
      body: JSON.stringify({ body_md }),
    }),
  activity: (issueKey: string) => request<Activity[]>(`/issues/${issueKey}/activity`),
  commits: (issueKey: string) => request<LinkedCommit[]>(`/issues/${issueKey}/commits`),
};
