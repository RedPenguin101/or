import { useState } from "react";
import { Link } from "react-router-dom";
import type { Dataset, Schema, Xform } from "../api";

// Mapping is the project's headline: how source schemas become outputs.
//
// Three sets — sources, the rules that bind them, and outputs — because that is
// the shape of the question a migrator is actually asking. Models and staging
// tables are implementation and deliberately absent; they live behind a rule.
export default function Mapping({
  projectId,
  xform,
  schemas,
  datasets,
}: {
  projectId: string;
  xform: Xform;
  schemas: Schema[];
  datasets: Dataset[];
}) {
  const [hot, setHot] = useState<string | null>(null);

  const rulesFor = (schemaRef: string) => xform.rules.filter((r) => r.from.includes(schemaRef));
  const rulesTo = (target: string) => xform.rules.filter((r) => r.to === target);

  // Highlighting is how the binding is shown. Drawing connector lines between
  // variable-height cards needs measurement and breaks on reflow; lighting up
  // both ends of a relationship says the same thing and survives any layout.
  const lit = (kind: "schema" | "rule" | "output", key: string) => {
    if (!hot) return false;
    const [hk, hv] = hot.split("|");
    if (hk === kind && hv === key) return true;
    if (hk === "rule") {
      const r = xform.rules.find((x) => x.name === hv);
      if (!r) return false;
      return (kind === "schema" && r.from.includes(key)) || (kind === "output" && r.to === key);
    }
    if (hk === "schema" && kind === "rule") return rulesFor(hv).some((r) => r.name === key);
    if (hk === "output" && kind === "rule") return rulesTo(hv).some((r) => r.name === key);
    return false;
  };

  const cls = (on: boolean) =>
    `block rounded-lg border p-3 transition-colors ${
      on ? "border-accent bg-accent-soft" : "border-line bg-surface hover:border-ink-soft"
    }`;

  return (
    <div className="grid gap-4 md:grid-cols-[1fr_1.35fr_1fr]">
      <Column title="Source schemas" count={schemas.length}>
        {schemas.map((s) => {
          const ref = `${s.source_system}/${s.schema}`;
          const ds = datasets.filter((d) => d.schema_ref === ref);
          const rows = ds.reduce((n, d) => n + d.rows, 0);
          return (
            <Link
              key={ref}
              to={`/p/${projectId}/s/${encodeURIComponent(ref)}`}
              className={cls(lit("schema", ref))}
              onMouseEnter={() => setHot(`schema|${ref}`)}
              onMouseLeave={() => setHot(null)}
            >
              <div className="mono text-sm font-medium">{s.schema}</div>
              <div className="text-xs text-ink-soft">{s.source_system}</div>
              <div className="mt-1.5 text-xs text-ink-soft">
                {s.fields.length} fields
                {ds.length > 0 ? ` · ${rows} rows` : " · no data yet"}
              </div>
            </Link>
          );
        })}
      </Column>

      <Column title="Transformation rules" count={xform.rules.length}>
        {xform.rules.map((r) => (
          <Link
            key={r.name}
            to={`/p/${projectId}/x/${r.name}`}
            className={cls(lit("rule", r.name))}
            onMouseEnter={() => setHot(`rule|${r.name}`)}
            onMouseLeave={() => setHot(null)}
          >
            <div className="text-sm font-medium">{r.title}</div>
            <p className="mt-1 line-clamp-3 text-xs text-ink-soft">{r.summary}</p>
            <div className="mt-2 flex flex-wrap items-center gap-1.5 text-[11px]">
              {r.from.map((f) => (
                <span key={f} className="mono rounded bg-raised px-1.5 py-0.5 text-ink-soft">
                  {f.split("/")[1] ?? f}
                </span>
              ))}
              <span className="text-ink-soft">→</span>
              <span className="mono rounded bg-raised px-1.5 py-0.5 text-ink-soft">{r.to}</span>
            </div>
          </Link>
        ))}
      </Column>

      <Column title="Targets" count={xform.targets.length}>
        {xform.targets.map((t, i) => (
          <Link
            key={t}
            to={`/p/${projectId}/target?t=${encodeURIComponent(t)}`}
            className={cls(lit("output", t))}
            onMouseEnter={() => setHot(`output|${t}`)}
            onMouseLeave={() => setHot(null)}
          >
            <div className="mono text-sm font-medium break-all">{t}</div>
            <div className="mt-1.5 text-xs text-ink-soft">
              {rulesTo(t).length === 1 ? "1 rule writes" : `${rulesTo(t).length} rules write`} this
              {i > 0 ? " · loads later" : ""}
            </div>
          </Link>
        ))}
      </Column>
    </div>
  );
}

function Column({
  title,
  count,
  children,
}: {
  title: string;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <div>
      <div className="mb-2 flex items-baseline gap-2">
        <h3 className="text-[11px] font-semibold tracking-wide text-ink-soft uppercase">{title}</h3>
        <span className="text-[11px] text-ink-soft/70">{count}</span>
      </div>
      <div className="space-y-2">{children}</div>
    </div>
  );
}
