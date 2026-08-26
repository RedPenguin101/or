import { Link, Route, Routes, useLocation } from "react-router-dom";
import Projects from "./routes/Projects";
import Project from "./routes/Project";
import RunView from "./routes/Run";
import DatasetView from "./routes/Dataset";
import SchemaView from "./routes/Schema";
import RuleView from "./routes/Rule";
import OutputView from "./routes/Output";

export default function App() {
  const { pathname } = useLocation();
  return (
    <div className="min-h-screen">
      <header className="border-b border-line bg-raised">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-5 py-3">
          <Link to="/" className="text-sm font-semibold tracking-tight">
            OnRamp
          </Link>
          <span className="text-xs text-ink-soft">
            migration to Qniform &middot; iteration&nbsp;1
          </span>
          {pathname !== "/" && (
            <Link to="/" className="ml-auto text-xs text-accent hover:underline">
              All projects
            </Link>
          )}
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-5 py-6">
        <Routes>
          <Route path="/" element={<Projects />} />
          <Route path="/p/:id" element={<Project />} />
          <Route path="/p/:id/s/:ref" element={<SchemaView />} />
          <Route path="/p/:id/x/:rule" element={<RuleView />} />
          <Route path="/p/:id/o/:name" element={<OutputView />} />
          <Route path="/p/:id/d/:did" element={<DatasetView />} />
          <Route path="/p/:id/r/:rid" element={<RunView />} />
        </Routes>
      </main>
    </div>
  );
}
