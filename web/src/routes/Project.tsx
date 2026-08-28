import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { Empty, ErrorNote, Hash, Panel, Pill } from "../components/ui";
import Mapping from "../components/Mapping";

// The project home answers two questions and no others: how does this source
// become that target, and what has happened so far. Schemas, datasets and rules
// all have their own pages, reached from the mapping above.
export default function Project() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const nav = useNavigate();

  const project = useQuery({ queryKey: ["p", id], queryFn: () => api.project(id) });
  const schemas = useQuery({ queryKey: ["p", id, "schemas"], queryFn: () => api.schemas(id) });
  const datasets = useQuery({ queryKey: ["p", id, "ds"], queryFn: () => api.datasets(id) });
  const runs = useQuery({ queryKey: ["p", id, "runs"], queryFn: () => api.runs(id) });
  const xform = useQuery({ queryKey: ["p", id, "xform"], queryFn: () => api.xform(id) });

  const [reason, setReason] = useState("");

  const run = useMutation({
    mutationFn: () => api.createRun(id, { reason, actor: "joe" }),
    onSuccess: (r) => {
      setReason("");
      qc.invalidateQueries({ queryKey: ["p", id, "runs"] });
      nav(`/p/${id}/r/${r.id}`);
    },
  });

  const hasData = (datasets.data?.length ?? 0) > 0;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold tracking-tight">{project.data?.name}</h1>
        {project.data && <Pill>{project.data.migration_type}</Pill>}
        <span className="text-xs text-ink-soft">target firm {project.data?.firm}</span>
        {xform.data && (
          <span className="ml-auto flex items-center gap-2 text-xs text-ink-soft">
            rules <Hash value={xform.data.hash} />
          </span>
        )}
      </div>

      <Panel
        title="How the source becomes Qniform"
        aside="click anything to open it"
      >
        {xform.data && schemas.data && datasets.data && (
          <Mapping
            projectId={id}
            xform={xform.data}
            schemas={schemas.data}
            datasets={datasets.data}
          />
        )}
      </Panel>

      <Panel title="Runs">
        <form
          className="mb-4 flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (reason.trim()) run.mutate();
          }}
        >
          <label className="flex-1 text-xs text-ink-soft">
            <div className="mb-1">
              Why are you running this?{" "}
              <span className="text-ink-soft/70">
                Required — a run nobody can explain in six months is the failure the audit trail
                exists to prevent.
              </span>
            </div>
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Check rep name conflict handling"
              className="w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink"
            />
          </label>
          <button
            type="submit"
            disabled={!reason.trim() || run.isPending || !hasData}
            className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
          >
            {run.isPending ? "Running…" : "Run"}
          </button>
        </form>
        {!hasData && (
          <p className="mb-3 text-xs text-ink-soft">
            No data yet — open a source schema above to upload a dataset for it.
          </p>
        )}
        {run.error && <ErrorNote error={run.error} />}
        {runs.data?.length === 0 && <Empty>No runs yet.</Empty>}
        <ul className="divide-y divide-line">
          {runs.data?.map((r) => (
            <li key={r.id}>
              <Link
                to={`/p/${id}/r/${r.id}`}
                className="flex items-center gap-3 py-2.5 hover:text-accent"
              >
                <span className="mono text-sm">#{r.number}</span>
                <Pill tone={r.status === "clean" ? "good" : r.status === "blocked" ? "bad" : "warn"}>
                  {r.status}
                </Pill>
                <span className="text-sm">{r.reason}</span>
                <span className="ml-auto flex items-center gap-3 text-xs text-ink-soft">
                  <span className="text-good">{r.accepted} accepted</span>
                  {r.rejected > 0 && <span className="text-warn">{r.rejected} withheld</span>}
                  <Hash value={r.digest} />
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}
