import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Project } from "./api";

// Selected project lives at the shell level so the sidebar switcher and the issue
// list share one source of truth. Persisted to localStorage across reloads.
interface ProjectCtx {
  projects: Project[];
  isLoading: boolean;
  projectKey: string;
  current?: Project;
  setProjectKey: (key: string) => void;
}

const Ctx = createContext<ProjectCtx | null>(null);
const STORE_KEY = "obt_project";

export function ProjectProvider({ children }: { children: ReactNode }) {
  const projectsQ = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });
  const projects = projectsQ.data?.items ?? [];
  const [projectKey, setKey] = useState(() => localStorage.getItem(STORE_KEY) ?? "");

  // Fall back to the first project once loaded, or when the stored key no longer exists.
  useEffect(() => {
    if (!projects.length) return;
    if (!projectKey || !projects.some((p) => p.key === projectKey)) setKey(projects[0].key);
  }, [projects, projectKey]);

  const setProjectKey = (key: string) => {
    setKey(key);
    localStorage.setItem(STORE_KEY, key);
  };

  const current = projects.find((p) => p.key === projectKey);

  return (
    <Ctx.Provider value={{ projects, isLoading: projectsQ.isLoading, projectKey, current, setProjectKey }}>
      {children}
    </Ctx.Provider>
  );
}

export function useProject(): ProjectCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useProject must be used within a ProjectProvider");
  return ctx;
}
