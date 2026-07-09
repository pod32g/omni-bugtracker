import { createBrowserRouter } from "react-router-dom";
import { Layout } from "./components/Layout";
import { Dashboard } from "./features/dashboard/Dashboard";
import { Board } from "./features/board/Board";
import { IssueList } from "./features/issues/IssueList";
import { IssueDetail } from "./features/issues/IssueDetail";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <Dashboard /> },
      { path: "board", element: <Board /> },
      { path: "issues", element: <IssueList /> },
      { path: "issues/:issueKey", element: <IssueDetail /> },
    ],
  },
]);
