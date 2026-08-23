import { Inbox } from "./features/inbox/Inbox";
import { MyWork } from "./features/mywork/MyWork";
import { Reports } from "./features/reports/Reports";
import { Navigate, createBrowserRouter } from "react-router-dom";
import { Layout } from "./components/Layout";
import { RouteError } from "./components/RouteError";
import { Dashboard } from "./features/dashboard/Dashboard";
import { Board } from "./features/board/Board";
import { IssueList } from "./features/issues/IssueList";
import { IssueDetail } from "./features/issues/IssueDetail";
import { RequireAccess, Settings } from "./features/settings/Settings";
import { ProfileSection } from "./features/settings/sections/Profile";
import { NotificationsSection } from "./features/settings/sections/Notifications";
import { TokensSection } from "./features/settings/sections/Tokens";
import { ViewsSection } from "./features/settings/sections/Views";
import { LabelsSection } from "./features/settings/sections/Labels";
import { AutomationSection } from "./features/settings/sections/Automation";
import { WebhooksSection } from "./features/settings/sections/Webhooks";
import { MembersSection } from "./features/settings/sections/Members";
import { IntegrationsSection } from "./features/settings/sections/Integrations";
import { ArchiveSection } from "./features/settings/sections/Archive";
import { OperationsSection } from "./features/settings/sections/Operations";
import { AuditSection } from "./features/settings/sections/Audit";
import { ProjectSettings } from "./features/projects/ProjectSettings";
import { Iterations } from "./features/iterations/Iterations";
import { Milestones } from "./features/milestones/Milestones";
import { Releases } from "./features/releases/Releases";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    // Renders inside the Layout, so a failed screen keeps the nav and the user keeps
    // their bearings. Every child route inherits this unless it sets its own.
    errorElement: <RouteError />,
    children: [
      { index: true, element: <Dashboard /> },
      { path: "board", element: <Board /> },
      { path: "my", element: <MyWork /> },
      { path: "inbox", element: <Inbox /> },
      { path: "issues", element: <IssueList /> },
      { path: "issues/:issueKey", element: <IssueDetail /> },
      { path: "reports", element: <Reports /> },
      { path: "iterations", element: <Iterations /> },
      { path: "milestones", element: <Milestones /> },
      { path: "releases", element: <Releases /> },
      {
        path: "settings",
        element: <Settings />,
        children: [
          { index: true, element: <Navigate to="profile" replace /> },
          { path: "profile", element: <ProfileSection /> },
          { path: "notifications", element: <NotificationsSection /> },
          { path: "tokens", element: <TokensSection /> },
          { path: "views", element: <ViewsSection /> },
          {
            path: "labels",
            element: (
              <RequireAccess section="labels">
                <LabelsSection />
              </RequireAccess>
            ),
          },
          {
            path: "automation",
            element: (
              <RequireAccess section="automation">
                <AutomationSection />
              </RequireAccess>
            ),
          },
          {
            path: "webhooks",
            element: (
              <RequireAccess section="webhooks">
                <WebhooksSection />
              </RequireAccess>
            ),
          },
          {
            path: "members",
            element: (
              <RequireAccess section="members">
                <MembersSection />
              </RequireAccess>
            ),
          },
          {
            path: "integrations",
            element: (
              <RequireAccess section="integrations">
                <IntegrationsSection />
              </RequireAccess>
            ),
          },
          {
            path: "archive",
            element: (
              <RequireAccess section="archive">
                <ArchiveSection />
              </RequireAccess>
            ),
          },
          {
            path: "operations",
            element: (
              <RequireAccess section="operations">
                <OperationsSection />
              </RequireAccess>
            ),
          },
          {
            path: "audit",
            element: (
              <RequireAccess section="audit">
                <AuditSection />
              </RequireAccess>
            ),
          },
        ],
      },
      { path: "projects/:key/settings", element: <ProjectSettings /> },
    ],
  },
]);
