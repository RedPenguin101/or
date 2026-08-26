import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { ErrorNote, Hash, Panel, Pill } from "../components/ui";
import Rejects from "../components/Rejects";

export default function DatasetView() {
  const { id = "", did = "" } = useParams();
  const q = useQuery({
    queryKey: ["p", id, "ds", did],
    queryFn: () => api.datasetRows(id, did),
  });

  if (q.error) return <ErrorNote error={q.error} />;
  if (!q.data) return null;
  const { dataset: d, rows } = q.data;
  const cols = rows.length ? Object.keys(rows[0]) : [];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="mono text-xl font-semibold tracking-tight">{d.name}</h1>
        <Hash value={d.hash} />
        <Pill>{d.schema_ref}</Pill>
        <span className="text-xs text-ink-soft">
          {d.rows} rows
          {d.rejected > 0 && `, ${d.rejected} unreadable`}
        </span>
      </div>

      {d.rejected > 0 && (
        <Panel title="Could not be read" aside="these rows never became part of the dataset, so no rule saw them">
          <Rejects rejects={d.rejects} />
        </Panel>
      )}

      <Panel
        title="Rows"
        aside="typed and decoded — this is the dataset the rules read"
      >
        <div className="overflow-x-auto">
          <table className="mono w-full text-[11px] whitespace-nowrap">
            <thead className="text-left text-ink-soft">
              <tr className="border-b border-line">
                {cols.map((c) => (
                  <th key={c} className="py-1.5 pr-3 font-medium">
                    {c}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i} className="border-b border-line/60">
                  {cols.map((c) => (
                    <td key={c} className="py-1 pr-3">
                      {r[c] === null ? (
                        <span className="text-ink-soft/50" title="absent — matched a null sentinel">
                          ·
                        </span>
                      ) : (
                        String(r[c])
                      )}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </div>
  );
}
