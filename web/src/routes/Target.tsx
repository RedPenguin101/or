import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { ErrorNote, Panel, Pill, Stat } from "../components/ui";
import { Inline } from "../components/Prose";

// An Output as the target defines it, against what the rules actually send.
//
// The coverage table is the point. A migration's real backlog is the set of
// fields Qniform accepts that nothing yet fills, and that list is otherwise
// carried in someone's head.
export default function TargetView() {
  const { id = "" } = useParams();
  const [params] = useSearchParams();
  const target = params.get("t") ?? "";
  const q = useQuery({
    queryKey: ["p", id, "target", target],
    queryFn: () => api.target(id, target),
    enabled: target !== "",
  });
  const [showDoc, setShowDoc] = useState(false);

  if (q.error) return <ErrorNote error={q.error} />;
  if (!q.data) return null;
  const o = q.data;
  const bound = o.fields.filter((f) => f.bound.length > 0);
  const unbound = o.fields.filter((f) => f.bound.length === 0);
  const missingRequired = unbound.filter((f) => f.required);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="mono text-xl font-semibold tracking-tight">{o.target}</h1>
        <Pill>target</Pill>
        <Link to={`/p/${id}`} className="ml-auto text-xs text-accent hover:underline">
          ← project
        </Link>
      </div>

      {o.doc_error && <ErrorNote error={o.doc_error} />}

      <Panel
        title="Coverage"
        aside={o.doc ? `${o.doc.max_items} items per request` : undefined}
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
            <thead className="bg-chrome-soft text-left text-ink-soft">
              <tr className="border-b border-line">
                <th className="py-1.5 pr-3 font-medium">Target field</th>
                <th className="py-1.5 pr-3 font-medium">Type</th>
                <th className="py-1.5 pr-3 font-medium">Written by</th>
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
                    {f.bound.length > 0 ? (
                      <span className="text-good">{f.bound.join(", ")}</span>
                    ) : (
                      <span className="text-ink-soft/60">not populated</span>
                    )}
                    {f.key_from && (
                      <div className="text-[10px] text-ink-soft/70">key from {f.key_from}</div>
                    )}
                    {f.literal && <div className="text-[10px] text-ink-soft/70">a literal</div>}
                    {f.references && (
                      <div className="text-[10px] text-ink-soft/70">
                        identity minted by {f.references}
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

      <Panel title="Written by">
        <ul className="space-y-1.5 text-sm">
          {o.rules.map((r) => (
            <li key={r.name}>
              <Link to={`/p/${id}/x/${r.name}`} className="hover:text-accent">
                <span className="font-medium">{r.title}</span>
                <span className="text-ink-soft">
                  {" "}
                  — {r.from.join(", ")}, minting <span className="mono">{r.mints.prefix}…</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
        <p className="mt-3 max-w-3xl text-xs text-ink-soft">
          Load position {o.load_rank} in Qniform&apos;s order. That ordering is a property of the
          target rather than of this project — the references here cannot resolve until whatever
          they name has landed — so it is recorded once for Qniform rather than declared per
          migration.
        </p>
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
