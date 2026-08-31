import { Link, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import Projects from "./routes/Projects";
import Project from "./routes/Project";
import RunView from "./routes/Run";
import DatasetView from "./routes/Dataset";
import SchemaView from "./routes/Schema";
import RuleView from "./routes/Rule";
import TargetView from "./routes/Target";

export default function App() {
  const { pathname } = useLocation();
  // Home is the project you are working in, not the list of projects: a
  // migration runs for months inside one of them. The list is reachable from
  // the project home, where switching is the thing you might actually want.
  const projectID = pathname.match(/^\/p\/([^/]+)/)?.[1];
  const atProjectHome = !!projectID && pathname === `/p/${projectID}`;
  const project = useQuery({
    queryKey: ["p", projectID],
    queryFn: () => api.project(projectID!),
    enabled: !!projectID,
  });

  return (
    <div className="min-h-screen">
      <header className="border-b border-line bg-chrome">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-5 py-3">
          <Link
            to={projectID ? `/p/${projectID}` : "/"}
            className="text-sm font-semibold tracking-tight"
          >
            OnRamp
          </Link>
          {projectID ? (
            <>
              <span className="text-xs text-ink-soft">/</span>
              <Link
                to={`/p/${projectID}`}
                className="text-sm tracking-tight hover:text-accent"
              >
                {project.data?.name ?? " "}
              </Link>
            </>
          ) : (
            <span className="text-xs text-ink-soft">
              migration to Qniform &middot; iteration&nbsp;1
            </span>
          )}
          {atProjectHome && (
            <Link
              to="/"
              className="ml-auto text-xs text-ink-soft hover:text-accent"
            >
              All projects
            </Link>
          )}
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-5 py-6">
        <Routes>
          <Route path="/" element={<Projects />} />
          <Route path="/p/:id" element={<Project />} />
          <Route path="/p/:id/s/:system/:name" element={<SchemaView />} />
          <Route path="/p/:id/x/:rule" element={<RuleView />} />
          <Route path="/p/:id/target" element={<TargetView />} />
          <Route path="/p/:id/d/:did" element={<DatasetView />} />
          <Route path="/p/:id/r/:rid" element={<RunView />} />
        </Routes>
      </main>
    </div>
  );
}
