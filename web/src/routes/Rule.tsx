import type { ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, schemaKey, schemaPath, type Bind } from "../api";
import { ErrorNote, Panel, Pill } from "../components/ui";
import Sql from "../components/Sql";

// One rule, in three facts: what it maps between, the SQL that does it, and the
// JSON that leaves. The prose a rule carries lives on the pages that survey many
// of them; here the reader has already chosen this rule and wants the mechanism.
export default function RuleView() {
  const { id = "", rule = "" } = useParams();
  const transform = useQuery({
    queryKey: ["p", id, "transform"],
    queryFn: () => api.transform(id),
  });

  const r = transform.data?.rules.find((x) => x.name === rule);
  if (transform.isSuccess && !r) return <ErrorNote error={`No rule ${rule}`} />;
  if (!r || !transform.data) return null;

  // The emitter built this payload, so the page reads it rather than working
  // out what the payload would be: one implementation, and the field order is
  // the one a run actually sends.
  const payload = Object.entries(transform.data.payloads[r.name] ?? {});

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold tracking-tight">{r.title}</h1>
        <Pill>rule</Pill>
        <Link
          to={`/p/${id}`}
          className="ml-auto text-xs text-accent hover:underline"
        >
          ← project
        </Link>
      </div>

      <Panel title="Maps">
        <dl className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-baseline gap-x-4 gap-y-2 text-xs">
          <Fact label="from">
            <div className="flex flex-wrap gap-2">
              {r.from.map((f) => (
                <Link
                  key={schemaKey(f)}
                  to={schemaPath(id, f)}
                  className={chip}
                >
                  {schemaKey(f)}
                </Link>
              ))}
            </div>
          </Fact>
          <Fact label="to">
            <Link
              to={`/p/${id}/target?t=${encodeURIComponent(r.to)}`}
              className={chip}
            >
              {r.to}
            </Link>
          </Fact>
          <Fact label="mints">
            <span className="mono">{r.mints.entity}</span>
            <span className="text-ink-soft"> as </span>
            <span className="mono text-accent">
              {r.mints.prefix || "(no prefix)"}
            </span>
            <span className="text-ink-soft"> + </span>
            <span className="mono">{r.mints.from.join(" + ")}</span>
          </Fact>
          {r.mints.conflict && (
            <Fact label="conflict">
              <span className="text-ink-soft">rows disagreeing on </span>
              <span className="mono">{r.mints.conflict.on.join(", ")}</span>
              <span className="text-ink-soft"> → </span>
              <span className="mono">{r.mints.conflict.resolve}</span>
            </Fact>
          )}
        </dl>
      </Panel>

      <Panel title="SQL" aside={`sql/${r.name}.sql`}>
        <Sql src={transform.data.sql[r.name]} />
      </Panel>

      <Panel
        title="Payload"
        aside={`one per ${r.binding.row_key} · a NULL column is left out`}
      >
        <div className="mono overflow-x-auto rounded-md border border-line bg-surface p-3 text-[11px] leading-relaxed">
          <div>{"{"}</div>
          {payload.map(([name, value], i) => (
            <div
              key={name}
              className="flex flex-wrap items-baseline justify-between gap-x-8 pl-4"
            >
              <div>
                <span className="text-syn-string">&quot;{name}&quot;</span>:{" "}
                <Value json={JSON.stringify(value)} />
                {i < payload.length - 1 && ","}
              </div>
              <Note bind={r.binding.fields[name]} projectID={id} />
            </div>
          ))}
          <div>{"}"}</div>
        </div>
      </Panel>
    </div>
  );
}

const chip =
  "mono rounded-md border border-line px-2 py-1 hover:border-accent hover:text-accent";

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="mono text-ink-soft">{label}</dt>
      <dd>{children}</dd>
    </>
  );
}

// The payload as the backend rendered it. The one thing this knows about the
// emitter is its placeholder convention — a ⟨column⟩ marks where a row's value
// goes — which it paints as a column rather than as data. The shape, the order
// and the values are the emitter's own.
function Value({ json }: { json: string }) {
  return (
    <>
      {json.split(/(\u27e8[^\u27e9]*\u27e9)/).map((part, i) => (
        <span
          key={i}
          className={
            part.startsWith("\u27e8") ? "text-syn-ident" : "text-syn-string"
          }
        >
          {part}
        </span>
      ))}
    </>
  );
}

function Note({ bind, projectID }: { bind?: Bind; projectID: string }) {
  if (bind?.object) return <span className="text-ink-soft">literal</span>;
  if (bind?.key_from)
    return <span className="text-ink-soft">the column is the key</span>;
  if (bind?.references)
    return (
      <span className="text-ink-soft">
        identity of{" "}
        <Link
          to={`/p/${projectID}/x/${bind.references}`}
          className="text-accent hover:underline"
        >
          {bind.references}
        </Link>
      </span>
    );
  return null; // the ordinary case: the column of the same name, said once in the aside
}
