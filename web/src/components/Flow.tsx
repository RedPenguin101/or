import type { Run } from "../api";

// Flow makes the fan-out legible: one flat source file becoming several
// emissions, with what fell out at each step. This is the insight the product
// exists to deliver, so it gets shown before any payload detail.
export default function Flow({ run }: { run: Run }) {
  const withheldAtIngest = run.withheld_at_ingest.length;
  const outputs = run.outputs;

  // Run records are never migrated, so older ones lack fields added since. The
  // source counts cannot be reconstructed from the stages — derivation means no
  // stage's row count stands for the source's, which is the whole reason they
  // are recorded rather than inferred. So they are omitted, not guessed.
  const source = run.source;

  return (
    <div className="overflow-x-auto">
      <div className="flex min-w-max items-stretch gap-2">
        {source ? (
          <>
            <Node
              label="File"
              value={source.delivered}
              unit="rows delivered"
              sub="CSV, as it arrived"
              tone={withheldAtIngest ? "warn" : "good"}
            />
            <Arrow />
            <Node
              label="Dataset"
              value={source.rows}
              unit="rows"
              sub={
                withheldAtIngest
                  ? `${withheldAtIngest} could not be read`
                  : "every row read cleanly"
              }
              tone={withheldAtIngest ? "warn" : "good"}
              withheld={withheldAtIngest}
            />
            <Arrow />
          </>
        ) : null}

        {outputs.map((s, i) => (
          <div key={s.name} className="flex items-stretch gap-2">
            <Node
              label={s.title || s.name}
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

      {!source && (
        <p className="mt-2 max-w-3xl text-xs text-ink-soft/80">
          This run predates source counts being recorded, so the rows delivered and staged are not
          shown. They are not inferred from the outputs: derivation means no output&apos;s row
          count stands for the source&apos;s.
        </p>
      )}
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
