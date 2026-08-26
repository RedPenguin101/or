import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { ErrorNote, Panel, Pill } from "../components/ui";

// One rule, in full: what it does in prose, then how — identity, the binding to
// the target, and the SQL. The prose comes first because it is the part no
// amount of reading the SQL would tell you.
export default function RuleView() {
  const { id = "", rule = "" } = useParams();
  const xform = useQuery({ queryKey: ["p", id, "xform"], queryFn: () => api.xform(id) });

  const r = xform.data?.rules.find((x) => x.name === rule);
  if (xform.isSuccess && !r) return <ErrorNote error={`No rule ${rule}`} />;
  if (!r || !xform.data) return null;

  const emitter = xform.data.emitters[r.emitter];
  const fieldNames = emitter?.order ?? Object.keys(emitter?.fields ?? {});

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold tracking-tight">{r.title}</h1>
        <Pill>rule</Pill>
        <Link to={`/p/${id}`} className="ml-auto text-xs text-accent hover:underline">
          ← project
        </Link>
      </div>

      <Panel title="What it does">
        <p className="max-w-3xl text-sm leading-relaxed">{r.summary}</p>
        <div className="mt-4 flex flex-wrap items-center gap-2 text-xs">
          {r.from.map((f) => (
            <Link
              key={f}
              to={`/p/${id}/s/${encodeURIComponent(f)}`}
              className="mono rounded-md border border-line px-2 py-1 hover:border-accent hover:text-accent"
            >
              {f}
            </Link>
          ))}
          <span className="text-ink-soft">→</span>
          <span className="rounded-md border border-line px-2 py-1">
            {r.output_title}
            <span className="mono ml-2 text-ink-soft">{r.target}</span>
          </span>
        </div>
      </Panel>

      <Panel title="Identity">
        <div className="space-y-2 text-sm">
          <div>
            <span className="text-ink-soft">Minted as </span>
            <span className="mono">{r.key}</span>
          </div>
          {r.identity && (
            <>
              <div className="text-xs text-ink-soft">
                Identity is declared on the <span className="mono">{r.entity}</span> entity, at
                column <span className="mono">{r.identity.column}</span> — shared with every other
                rule producing {r.output_title.toLowerCase()}, which together derive it from{" "}
                <span className="mono">{r.identity.derived_from.join(", ")}</span>. Minting is
                ordinary SQL; declaring separately that the column <em>is</em> an identity is what
                lets the system check the mint is deterministic and detect disagreement generically,
                rather than each rule hand-rolling its own.
              </div>
              {r.identity.conflict && (
                <div className="rounded-md border border-warn/40 bg-warn-soft/40 p-3 text-xs">
                  When source rows disagree about{" "}
                  <span className="mono">{r.identity.conflict.on.join(", ")}</span> for the same
                  identity, the policy is{" "}
                  <span className="mono">{r.identity.conflict.resolve}</span>: the entity is
                  quarantined and everything naming it is withheld too. No single row is wrong —
                  the set is inconsistent, and picking a winner is a decision someone has to own.
                </div>
              )}
            </>
          )}
        </div>
      </Panel>

      {emitter && (
        <Panel
          title="Fields sent to Qniform"
          aside={`${emitter.batch.max_items} items per request, ${emitter.batch.failure_mode.replace("_", " ")}`}
        >
          <table className="w-full text-xs">
            <thead className="text-left text-ink-soft">
              <tr className="border-b border-line">
                <th className="py-1.5 pr-3 font-medium">Field</th>
                <th className="py-1.5 font-medium">Notes</th>
              </tr>
            </thead>
            <tbody>
              {fieldNames.map((name) => {
                const b = emitter.fields[name];
                return (
                  <tr key={name} className="border-b border-line/60 align-top">
                    <td className="mono py-1.5 pr-3 whitespace-nowrap">{name}</td>
                    <td className="py-1.5 text-ink-soft">
                      {b?.map ? (
                        <div>
                          Nested: the value in <span className="mono">{b.map.key_from}</span>{" "}
                          becomes the key. The one shape SQL cannot produce.
                        </div>
                      ) : (
                        <div>
                          Sent as-is from the model column of the same name, or left out entirely
                          when it is NULL.
                        </div>
                      )}
                      {b?.references && (
                        <div>
                          Carries a <span className="mono">{b.references}</span> identity, so a
                          withheld {b.references} withholds this row too.
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Panel>
      )}

      <Panel title="How it is computed" aside="implementation">
        <div className="space-y-3">
          {r.models.map((m) => (
            <div key={m}>
              <div className="mono mb-1 text-xs text-ink-soft">{m}.sql</div>
              <pre className="mono overflow-x-auto rounded-md border border-line bg-surface p-3 text-[11px] leading-relaxed">
                {xform.data!.models[m]}
              </pre>
              {xform.data!.model_deps[m]?.length ? (
                <div className="mono mt-1 text-[11px] text-ink-soft">
                  reads {xform.data!.model_deps[m].join(", ")}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}
