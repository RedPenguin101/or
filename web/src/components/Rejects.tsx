import type { Reject } from "../api";
import { Pill } from "./ui";

// Rejects are grouped by cause, so the report reads "10 accounts withheld
// because two reps conflict" rather than "10 accounts failed". With staged
// emission that difference is the whole value of the error report.
export default function Rejects({ rejects }: { rejects: Reject[] }) {
  const direct = rejects.filter((r) => !r.cause);
  const caused = new Map<string, Reject[]>();
  for (const r of rejects) {
    if (!r.cause) continue;
    const k = `${r.cause.output}:${r.cause.identity}`;
    caused.set(k, [...(caused.get(k) ?? []), r]);
  }

  return (
    <div className="space-y-3">
      {direct.map((r, i) => (
        <Card key={i} r={r} />
      ))}
      {[...caused.entries()].map(([key, rows]) => (
        <div key={key} className="rounded-md border border-warn/40 bg-warn-soft/40 p-3">
          <div className="mb-2 text-xs">
            <span className="font-medium">{rows.length}</span> withheld because{" "}
            <span className="mono">{key.split(":")[1]}</span> is quarantined in{" "}
            <span className="mono">{key.split(":")[0]}</span> — not because anything is wrong with
            these rows.
          </div>
          <ul className="mono flex flex-wrap gap-1.5 text-[11px]">
            {rows.map((r, i) => (
              <li key={i} className="rounded bg-raised px-1.5 py-0.5" title={r.message}>
                {r.identity}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function Card({ r }: { r: Reject }) {
  const tone = r.class === "conflict" ? "warn" : "bad";
  return (
    <div className="rounded-md border border-line bg-surface p-3">
      <div className="mb-1.5 flex flex-wrap items-center gap-2 text-xs">
        <Pill tone={tone}>{r.class}</Pill>
        <span className="text-ink-soft">at {r.phase}</span>
        {r.identity && <span className="mono font-medium">{r.identity}</span>}
        {r.source_row ? <span className="text-ink-soft">source row {r.source_row}</span> : null}
      </div>
      <div className="text-sm">{r.message}</div>
      {r.values && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs">
          <span className="text-ink-soft">
            disagreeing values for <span className="mono">{r.field}</span>:
          </span>
          {r.values.map((v) => (
            <span key={v} className="mono rounded bg-warn-soft px-1.5 py-0.5 text-warn">
              {v}
            </span>
          ))}
        </div>
      )}
      {r.source_rows && (
        <div className="mono mt-1 text-[11px] text-ink-soft">
          from source rows {r.source_rows.join(", ")}
        </div>
      )}
    </div>
  );
}
