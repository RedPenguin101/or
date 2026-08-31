import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, schemaKey, schemaPath } from "../api";
import { ErrorNote, Panel, Pill } from "../components/ui";
import Sql from "../components/Sql";

// One rule, in full. A rule now carries its whole chain — the source it reads,
// the identity it mints, the SQL that flattens it, and the shape of the payload
// that leaves — so this page can show the entire path without joining anything
// onto it. The prose comes first because it is the part no amount of reading the
// SQL would tell you.
export default function RuleView() {
  const { id = "", rule = "" } = useParams();
  const transform = useQuery({ queryKey: ["p", id, "transform"], queryFn: () => api.transform(id) });

  const r = transform.data?.rules.find((x) => x.name === rule);
  if (transform.isSuccess && !r) return <ErrorNote error={`No rule ${rule}`} />;
  if (!r || !transform.data) return null;

  const fields = r.binding.order ?? Object.keys(r.binding.fields);

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
              key={schemaKey(f)}
              to={schemaPath(id, f)}
              className="mono rounded-md border border-line px-2 py-1 hover:border-accent hover:text-accent"
            >
              {schemaKey(f)}
            </Link>
          ))}
          <span className="text-ink-soft">→</span>
          <Link
            to={`/p/${id}/target?t=${encodeURIComponent(r.to)}`}
            className="mono rounded-md border border-line px-2 py-1 hover:border-accent hover:text-accent"
          >
            {r.to}
          </Link>
        </div>
      </Panel>

      <Panel title="Identity it mints" aside={`entity: ${r.mints.entity}`}>
        <div className="space-y-2 text-sm">
          <div>
            <span className="mono text-accent">{r.mints.prefix || "(no prefix)"}</span>
            <span className="text-ink-soft"> + </span>
            <span className="mono">{r.mints.from.join(" + ")}</span>
          </div>
          <p className="max-w-3xl text-xs text-ink-soft">
            Declared rather than left in the SQL, because identity does far more than the expression
            suggests: it deduplicates, detects disagreement, resolves what other rules reference,
            drives the quarantine cascade, aligns one run against another for diffing, and is the
            closure a slice has to follow. Every one of those needs to know the mint without running
            it. The prefix keeps this rule&apos;s identities in their own key space, so two rules
            minting <span className="mono">{r.mints.entity}</span> can never name the same object.
          </p>
          {r.mints.conflict && (
            <div className="rounded-md border border-warn/40 bg-warn-soft/40 p-3 text-xs">
              When source rows disagree about{" "}
              <span className="mono">{r.mints.conflict.on.join(", ")}</span> for the same identity,
              the policy is <span className="mono">{r.mints.conflict.resolve}</span>: the entity is
              quarantined and everything naming it is withheld too. No single row is wrong — the set
              is inconsistent, and picking a winner is a decision someone has to own.
            </div>
          )}
        </div>
      </Panel>

      <Panel title="Fields sent to Qniform" aside={`row key: ${r.binding.row_key}`}>
        <table className="w-full text-xs">
          <thead className="text-left text-ink-soft">
            <tr className="border-b border-line">
              <th className="py-1.5 pr-3 font-medium">Field</th>
              <th className="py-1.5 font-medium">Notes</th>
            </tr>
          </thead>
          <tbody>
            {fields.map((name) => {
              const b = r.binding.fields[name];
              return (
                <tr key={name} className="border-b border-line/60 align-top">
                  <td className="mono py-1.5 pr-3 whitespace-nowrap">{name}</td>
                  <td className="py-1.5 text-ink-soft">
                    {b?.object ? (
                      <div>
                        A literal: <span className="mono">{JSON.stringify(b.object)}</span>. This
                        rule never varies it, so it says so outright rather than carrying a column.
                      </div>
                    ) : b?.key_from ? (
                      <div>
                        Nested: the value in <span className="mono">{b.key_from}</span> becomes the
                        key. The one shape SQL cannot produce.
                      </div>
                    ) : b?.references ? (
                      <div>
                        The model holds the raw source key; rule{" "}
                        <Link to={`/p/${id}/x/${b.references}`} className="text-accent hover:underline">
                          {b.references}
                        </Link>{" "}
                        supplies the prefix. The mint is written once, so the two cannot drift — and
                        if that identity is quarantined, this row is withheld with it.
                      </div>
                    ) : (
                      <div>
                        Sent as-is from the model column of the same name, or left out entirely when
                        it is NULL.
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Panel>

      <Panel title="How it is computed" aside={`sql/${r.name}.sql`}>
        <p className="mb-3 max-w-3xl text-xs text-ink-soft">
          Every decision this rule makes is here. Columns are named for the Qniform fields they
          become, and a NULL means the field is left out — so nothing downstream renames,
          reinterprets, or conditionally drops what this produces.
        </p>
        <Sql src={transform.data.sql[r.name]} />
        {transform.data.sql_reads[r.name]?.length ? (
          <div className="mono mt-1 text-[11px] text-ink-soft">
            reads {transform.data.sql_reads[r.name].join(", ")}
          </div>
        ) : null}
      </Panel>
    </div>
  );
}
