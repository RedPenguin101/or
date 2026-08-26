import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, type Output } from "../api";
import { ErrorNote, Hash, Panel, Pill, Stat } from "../components/ui";
import Flow from "../components/Flow";
import Rejects from "../components/Rejects";

export default function RunView() {
  const { rid = "" } = useParams();
  const run = useQuery({ queryKey: ["run", rid], queryFn: () => api.run(rid) });

  if (run.error) return <ErrorNote error={run.error} />;
  if (!run.data) return null;
  const r = run.data;
  const status = r.totals.rejected === 0 ? "clean" : r.totals.accepted === 0 ? "blocked" : "partial";

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold tracking-tight">Run #{r.run}</h1>
        <Pill tone={status === "clean" ? "good" : status === "blocked" ? "bad" : "warn"}>
          {status}
        </Pill>
        <span className="text-sm text-ink-soft">{r.reason}</span>
        <span className="ml-auto text-xs text-ink-soft">
          {r.actor.id} &middot; {r.started_at.slice(0, 19).replace("T", " ")}
        </span>
      </div>

      <Panel
        title="Inputs"
        aside="Output = Apply(SourceSchemas, SourceDatasets, Xforms)"
      >
        <div className="flex flex-wrap items-start gap-8 text-xs">
          <div>
            <div className="mb-1 text-ink-soft">Schemas</div>
            {Object.entries(r.inputs.schemas).map(([k, v]) => (
              <div key={k} className="mono flex items-center gap-2">
                {k} <Hash value={v} />
              </div>
            ))}
          </div>
          <div>
            <div className="mb-1 text-ink-soft">Datasets</div>
            {Object.entries(r.inputs.datasets).map(([k, v]) => (
              <div key={k} className="mono flex items-center gap-2">
                {k} <Hash value={v} />
              </div>
            ))}
          </div>
          <div>
            <div className="mb-1 text-ink-soft">Xform</div>
            <Hash value={r.inputs.xform} />
          </div>
          <div>
            <div className="mb-1 text-ink-soft">Run identity</div>
            <Hash value={r.inputs.triple} />
            <div className="mt-1 max-w-xs text-ink-soft/80">
              Any run with this triple produced exactly this output.
            </div>
          </div>
        </div>
      </Panel>

      <Panel title="What happened">
        <div className="mb-5 flex gap-10">
          <Stat label="emitted" value={r.totals.emitted} />
          <Stat label="accepted" value={r.totals.accepted} tone="good" />
          <Stat
            label="withheld"
            value={r.totals.rejected}
            tone={r.totals.rejected ? "warn" : undefined}
          />
        </div>
        <Flow run={r} />
      </Panel>

      {r.withheld_at_ingest.length > 0 && (
        <Panel
          title="Withheld at ingest"
          aside="the file could not be read here, so no rule ever saw these rows"
        >
          <Rejects rejects={r.withheld_at_ingest} />
        </Panel>
      )}

      {r.outputs.map((o) => (
        <OutputPanel key={o.name} output={o} />
      ))}

      <Panel title="Models" aside="hashes fold in their inputs, so a change invalidates only what depends on it">
        <table className="w-full text-xs">
          <thead className="text-left text-ink-soft">
            <tr className="border-b border-line">
              <th className="py-1.5 pr-3 font-medium">Model</th>
              <th className="py-1.5 pr-3 font-medium">Rows out</th>
              <th className="py-1.5 pr-3 font-medium">Time</th>
              <th className="py-1.5 pr-3 font-medium">Cached</th>
              <th className="py-1.5 font-medium">Node hash</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(r.nodes).map(([name, n]) => (
              <tr key={name} className="border-b border-line/60">
                <td className="mono py-1.5 pr-3">{name}</td>
                <td className="py-1.5 pr-3 tabular-nums">{n.rows_out}</td>
                <td className="py-1.5 pr-3 tabular-nums text-ink-soft">{n.ms}ms</td>
                <td className="py-1.5 pr-3 text-ink-soft">{n.cached ? "yes" : "no"}</td>
                <td className="py-1.5">
                  <Hash value={n.hash} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

function OutputPanel({ output: s }: { output: Output }) {
  const [tab, setTab] = useState<"payloads" | "rejects">(
    s.rejected > 0 && s.accepted === 0 ? "rejects" : "payloads",
  );
  const payloads = s.payloads ?? [];
  const rejects = s.rejects ?? [];

  return (
    <Panel
      title={
        <span className="flex items-center gap-2">
          {s.title || s.name}
          <span className="mono text-xs font-normal text-ink-soft">{s.target}</span>
        </span>
      }
      aside={
        <span className="flex items-center gap-3">
          <span className="text-good">{s.accepted} accepted</span>
          {s.rejected > 0 && <span className="text-warn">{s.rejected} withheld</span>}
          {s.requests > 0 && (
            <span title="Qniform caps these endpoints at 500 items per request">
              {s.requests} request{s.requests === 1 ? "" : "s"}
            </span>
          )}
        </span>
      }
    >
      <div className="mb-3 flex gap-1 text-xs">
        {(["payloads", "rejects"] as const).map((t) => {
          const n = t === "payloads" ? payloads.length : rejects.length;
          if (n === 0) return null;
          return (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`rounded-md px-2.5 py-1 font-medium ${
                tab === t ? "bg-accent text-white" : "bg-surface text-ink-soft hover:text-ink"
              }`}
            >
              {t === "payloads" ? "Payloads" : "Withheld"} ({n})
            </button>
          );
        })}
      </div>

      {tab === "payloads" ? (
        payloads.length === 0 ? (
          <p className="text-sm text-ink-soft">Nothing emitted.</p>
        ) : (
          <div className="space-y-1.5">
            {payloads.map((p, i) => (
              <Payload key={i} value={p} />
            ))}
          </div>
        )
      ) : (
        <Rejects rejects={rejects} />
      )}
    </Panel>
  );
}

// Payload renders one request-body item. Field order is the emitter's, and an
// absent field is simply not shown — which is the point: absence is meaningful
// to Qniform and must not be rendered as an explicit null.
function Payload({ value }: { value: Record<string, unknown> }) {
  return (
    <div className="mono overflow-x-auto rounded-md border border-line bg-surface px-3 py-2 text-xs">
      <span className="text-ink-soft">{"{ "}</span>
      {Object.entries(value).map(([k, v], i, arr) => (
        <span key={k}>
          <span className="text-accent">{k}</span>
          <span className="text-ink-soft">: </span>
          <Value v={v} />
          {i < arr.length - 1 && <span className="text-ink-soft">, </span>}
        </span>
      ))}
      <span className="text-ink-soft">{" }"}</span>
    </div>
  );
}

function Value({ v }: { v: unknown }) {
  if (v === null) return <span className="text-bad">null</span>;
  if (typeof v === "object") {
    const entries = Object.entries(v as Record<string, unknown>);
    return (
      <span>
        <span className="text-ink-soft">{"{"}</span>
        {entries.map(([k, iv], i) => (
          <span key={k}>
            <span className="text-good">{k}</span>
            <span className="text-ink-soft">: </span>
            <Value v={iv} />
            {i < entries.length - 1 && <span className="text-ink-soft">, </span>}
          </span>
        ))}
        <span className="text-ink-soft">{"}"}</span>
      </span>
    );
  }
  return <span>{JSON.stringify(v)}</span>;
}
