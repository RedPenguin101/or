import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { ErrorNote, Panel, Pill, Stat } from "../components/ui";
import { Inline } from "../components/Prose";

// An Output as the target defines it, against what the rules actually send.
//
// The coverage table is the point. A migration's real backlog is the set of
// fields Qniform accepts that nothing yet fills, and that list is otherwise
// carried in someone's head.
export default function OutputView() {
  const { id = "", name = "" } = useParams();
  const q = useQuery({ queryKey: ["p", id, "o", name], queryFn: () => api.output(id, name) });
  const [showDoc, setShowDoc] = useState(false);

  if (q.error) return <ErrorNote error={q.error} />;
  if (!q.data) return null;
  const o = q.data;
  const bound = o.fields.filter((f) => f.bound);
  const unbound = o.fields.filter((f) => !f.bound);
  const missingRequired = unbound.filter((f) => f.required);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold tracking-tight">{o.title || o.name}</h1>
        <Pill>output</Pill>
        <span className="mono text-xs text-ink-soft">{o.target}</span>
        <Link to={`/p/${id}`} className="ml-auto text-xs text-accent hover:underline">
          ← project
        </Link>
      </div>

      {o.doc_error && <ErrorNote error={o.doc_error} />}

      <Panel
        title="Coverage"
        aside={`${o.batch.max_items} items per request · ${o.batch.failure_mode.replace("_", " ")}`}
      >
        <div className="mb-4 flex flex-wrap gap-10">
          <Stat label="fields bound" value={`${bound.length} of ${o.fields.length}`} tone="good" />
          <Stat
            label="not populated"
            value={unbound.length}
            tone={unbound.length ? "warn" : undefined}
          />
          {missingRequired.length > 0 && (
            <Stat label="required and unbound" value={missingRequired.length} tone="bad" />
          )}
        </div>

        {o.unknown_fields.length > 0 && (
          <div className="mb-3 rounded-md border border-bad bg-bad-soft px-3 py-2 text-xs text-bad">
            Bound but not in the spec: <span className="mono">{o.unknown_fields.join(", ")}</span>.
            Qniform would refuse these.
          </div>
        )}

        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead className="text-left text-ink-soft">
              <tr className="border-b border-line">
                <th className="py-1.5 pr-3 font-medium">Target field</th>
                <th className="py-1.5 pr-3 font-medium">Type</th>
                <th className="py-1.5 pr-3 font-medium">Produced?</th>
                <th className="py-1.5 font-medium">What Qniform says</th>
              </tr>
            </thead>
            <tbody>
              {o.fields.map((f) => (
                <tr
                  key={f.name}
                  className={`border-b border-line/60 align-top ${f.bound ? "" : "opacity-60"}`}
                >
                  <td className="mono py-1.5 pr-3 whitespace-nowrap">
                    {f.name}
                    {f.required && (
                      <span className={f.bound ? "text-ink-soft" : "text-bad"} title="required">
                        {" "}
                        *
                      </span>
                    )}
                  </td>
                  <td className="mono py-1.5 pr-3 whitespace-nowrap text-ink-soft">
                    {f.type || "—"}
                    {f.enum && (
                      <div className="text-[10px] text-ink-soft/70">{f.enum.join(" | ")}</div>
                    )}
                  </td>
                  <td className="mono py-1.5 pr-3 whitespace-nowrap">
                    {f.bound ? (
                      <span className="text-good">
                        {f.key_from ? `key from ${f.key_from}` : "yes"}
                      </span>
                    ) : (
                      <span className="text-ink-soft/60">not populated</span>
                    )}
                    {f.references && (
                      <div className="text-[10px] text-ink-soft/70">
                        carries a {f.references} identity
                      </div>
                    )}
                  </td>
                  <td className="py-1.5 text-ink-soft">
                    <Inline text={(f.description || "").split("\n\n")[0]} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {unbound.length > 0 && (
          <p className="mt-3 max-w-3xl text-xs text-ink-soft/80">
            Unpopulated fields are not necessarily wrong — some are derived by Qniform, some belong
            to source files this project has not received. They are the mapping still to do, which
            is worth being able to see rather than remember.
          </p>
        )}
      </Panel>

      <Panel title="Fed by">
        <ul className="space-y-1.5 text-sm">
          {o.rules.map((r) => (
            <li key={r.name}>
              <Link to={`/p/${id}/x/${r.name}`} className="hover:text-accent">
                <span className="font-medium">{r.title}</span>
                <span className="text-ink-soft"> — {r.from.join(", ")}</span>
              </Link>
            </li>
          ))}
        </ul>
        {o.after?.length ? (
          <p className="mt-3 text-xs text-ink-soft">
            Loads after <span className="mono">{o.after.join(", ")}</span>. That ordering is a
            property of Qniform rather than of this project: the references here cannot resolve
            until the earlier output has landed.
          </p>
        ) : null}
      </Panel>

      {o.doc?.description && (
        <Panel
          title="Qniform's own documentation"
          aside={
            <button onClick={() => setShowDoc(!showDoc)} className="text-accent hover:underline">
              {showDoc ? "hide" : "show"}
            </button>
          }
        >
          {showDoc ? (
            <div className="max-w-3xl space-y-2 text-xs leading-relaxed text-ink-soft">
              {o.doc.description.split("\n\n").map((p, i) => (
                <p key={i}>
                  <Inline text={p} />
                </p>
              ))}
            </div>
          ) : (
            <p className="text-xs text-ink-soft">{o.doc.summary}</p>
          )}
        </Panel>
      )}
    </div>
  );
}
