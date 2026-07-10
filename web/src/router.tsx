import { createBrowserRouter } from "react-router-dom";
import { Layout } from "./components/Layout";
import { Dashboard } from "./features/dashboard/Dashboard";
import { Board } from "./features/board/Board";
import { IssueList } from "./features/issues/IssueList";
import { IssueDetail } from "./features/issues/IssueDetail";
import { Settings } from "./features/settings/Settings";
import { ProjectSettings } from "./features/projects/ProjectSettings";
import { Milestones } from "./features/milestones/Milestones";
import { Releases } from "./features/releases/Releases";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <Dashboard /> },
      { path: "board", element: <Board /> },
      { path: "issues", element: <IssueList /> },
      { path: "issues/:issueKey", element: <IssueDetail /> },
      { path: "milestones", element: <Milestones /> },
      { path: "releases", element: <Releases /> },
      { path: "settings", element: <Settings /> },
      { path: "projects/:key/settings", element: <ProjectSettings /> },
    ],
  },
]);
