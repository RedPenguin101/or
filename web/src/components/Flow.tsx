import type { Dataset, Run } from "../api";

// Flow makes the fan-out legible: one flat source file becoming several
// emissions, with what fell out at each step. This is the insight the product
// exists to deliver, so it gets shown before any payload detail.
//
// The row counts come from the Datasets themselves, looked up by the ids the Run
// records. They are not copied onto the Run: a Dataset owns how many of its rows
// coerced and how many did not, and one place should say so.
export default function Flow({ run, datasets }: { run: Run; datasets: Dataset[] }) {
  const { outputs } = run;
  const byID = new Map(datasets.map((d) => [d.id, d]));

  // One pair of nodes per input, not a sum across them: with an account file and
  // a transaction file, a combined "rows delivered" is a number about nothing.
  const inputs = Object.entries(run.read).map(([alias, id]) => ({
    alias,
    d: byID.get(id),
  }));

  return (
    <div className="overflow-x-auto">
      <div className="flex min-w-max items-stretch gap-2">
        {inputs.map(({ alias, d }) => (
          <div key={alias} className="flex items-stretch gap-2">
            {d ? (
              <>
                <Node
                  label="Delivered"
                  value={d.rows + d.rejected}
                  unit="rows"
                  sub={`${d.source_system}/${d.schema}`}
                  tone={d.rejected ? "warn" : "good"}
                />
                <Arrow />
                <Node
                  label="Dataset"
                  value={d.rows}
                  unit="rows"
                  sub={d.rejected ? `${d.rejected} could not be read` : "every row read cleanly"}
                  tone={d.rejected ? "warn" : "good"}
                  withheld={d.rejected}
                />
              </>
            ) : (
              // The Dataset this Run read is no longer here. Say so rather than
              // inferring a count from an output — derivation means no output's
              // row count stands for the source's.
              <Node label="Dataset" value={0} unit={alias} sub="no longer available" tone="warn" />
            )}
            <Arrow />
          </div>
        ))}

        {outputs.map((s, i) => (
          <div key={s.rule} className="flex items-stretch gap-2">
            <Node
              label={s.title || s.rule}
              value={s.accepted}
              unit={`of ${s.emitted} emitted`}
              sub={s.target}
              tone={s.rejected ? "warn" : "good"}
              withheld={s.rejected}
            />
            {i < outputs.length - 1 && <Arrow />}
          </div>
        ))}
      </div>

      <p className="mt-3 max-w-3xl text-xs text-ink-soft">
        One flat Account Master becomes {outputs.length} outputs in dependency order. FAS has no
        party file, so every party here was derived from the account records — the counts differ
        because derivation deduplicates, and because anything naming a withheld party is withheld
        with it.
      </p>

    </div>
  );
}

function Node({
  label,
  value,
  unit,
  sub,
  tone,
  withheld,
}: {
  label: string;
  value: number;
  unit: string;
  sub?: string;
  tone: "good" | "warn";
  withheld?: number;
}) {
  return (
    <div className="min-w-[9.5rem] rounded-lg border border-line bg-surface px-3 py-2.5">
      <div className="text-[11px] font-medium tracking-wide text-ink-soft uppercase">{label}</div>
      <div className={`text-2xl font-semibold tabular-nums ${tone === "warn" ? "" : "text-good"}`}>
        {value}
      </div>
      <div className="text-[11px] text-ink-soft">{unit}</div>
      {withheld ? (
        <div className="mt-1 text-[11px] font-medium text-warn">{withheld} withheld</div>
      ) : null}
      {sub && (
        <div className="mono mt-1 truncate text-[10px] text-ink-soft/80" title={sub}>
          {sub}
        </div>
      )}
    </div>
  );
}

function Arrow() {
  return (
    <div className="flex items-center px-1 text-ink-soft" aria-hidden>
      <svg width="20" height="12" viewBox="0 0 20 12" fill="none">
        <path
          d="M0 6h17m0 0-4-4m4 4-4 4"
          stroke="currentColor"
          strokeWidth="1.25"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
}
