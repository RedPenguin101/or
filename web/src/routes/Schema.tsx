import { useRef } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, schemaKey, sameSchema } from "../api";
import type { SchemaName } from "../api";
import { Empty, ErrorNote, Panel, Pill } from "../components/ui";
import { Description } from "../components/Prose";

// A Source Schema and its data belong on one page: the schema is a claim about
// what the file contains, the datasets are the files, and the interesting
// question is always whether the two agree.
export default function SchemaView() {
  const { id = "", system = "", name = "" } = useParams();
  const want: SchemaName = { source_system: decodeURIComponent(system), schema: decodeURIComponent(name) };
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);

  const schemas = useQuery({ queryKey: ["p", id, "schemas"], queryFn: () => api.schemas(id) });
  const datasets = useQuery({ queryKey: ["p", id, "ds"], queryFn: () => api.datasets(id) });
  const transform = useQuery({ queryKey: ["p", id, "transform"], queryFn: () => api.transform(id) });

  const upload = useMutation({
    mutationFn: (file: File) => {
      const fd = new FormData();
      fd.set("file", file);
      fd.set("source_system", want.source_system);
      fd.set("schema_name", want.schema);
      fd.set("actor", "joe");
      return api.uploadDataset(id, fd);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["p", id, "ds"] }),
  });

  const s = schemas.data?.find((x) => sameSchema(x, want));
  const mine = datasets.data?.filter((d) => sameSchema(d, want)) ?? [];
  const feeds = transform.data?.rules.filter((r) => r.from.some((f) => sameSchema(f, want))) ?? [];

  if (schemas.isSuccess && !s)
    return <ErrorNote error={`No schema ${schemaKey(want)} in this project`} />;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="mono text-xl font-semibold tracking-tight">{s?.schema}</h1>
        <Pill>{s?.source_system}</Pill>
        <Link to={`/p/${id}`} className="ml-auto text-xs text-accent hover:underline">
          ← project
        </Link>
      </div>
      {s && (
        <div className="max-w-3xl text-sm text-ink-soft">
          <Description text={s.description} />
        </div>
      )}

      {feeds.length > 0 && (
        <Panel title="What this feeds">
          <ul className="space-y-1.5 text-sm">
            {feeds.map((r) => (
              <li key={r.name}>
                <Link to={`/p/${id}/x/${r.name}`} className="hover:text-accent">
                  <span className="font-medium">{r.title}</span>
                  <span className="mono text-ink-soft"> → {r.to}</span>
                </Link>
              </li>
            ))}
          </ul>
        </Panel>
      )}

      <Panel
        title="Datasets"
        aside={
          <button
            onClick={() => fileRef.current?.click()}
            className="rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white"
          >
            Upload CSV
          </button>
        }
      >
        <input
          ref={fileRef}
          type="file"
          accept=".csv,text/csv"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) upload.mutate(f);
            e.target.value = "";
          }}
        />
        {upload.error && <ErrorNote error={upload.error} />}
        {mine.length === 0 && <Empty>Nothing uploaded against this schema yet.</Empty>}
        <ul className="divide-y divide-line">
          {mine.map((d) => (
            <li key={d.id}>
              <Link
                to={`/p/${id}/d/${d.id}`}
                className="flex items-center gap-3 py-2.5 hover:text-accent"
              >
                <span className="mono text-xs">
                  {d.created_at.slice(0, 16).replace("T", " ")}
                </span>
                <span className="text-xs text-ink-soft">{d.rows} rows</span>
                {d.rejected > 0 && <Pill tone="warn">{d.rejected} withheld at coercion</Pill>}
              </Link>
            </li>
          ))}
        </ul>
      </Panel>

      <Panel title="Fields" aside={`${s?.fields.length ?? 0} declared`}>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead className="bg-chrome-soft text-left text-ink-soft">
              <tr className="border-b border-line">
                <th className="py-1.5 pr-3 font-medium">Field</th>
                <th className="py-1.5 pr-3 font-medium">Type</th>
                <th className="py-1.5 pr-3 font-medium">Encoding</th>
                <th className="py-1.5 pr-3 font-medium">Means absent</th>
                <th className="py-1.5 font-medium">Description</th>
              </tr>
            </thead>
            <tbody>
              {s?.fields.map((f) => (
                <tr key={f.name} className="border-b border-line/60 align-top">
                  <td className="mono py-1.5 pr-3 whitespace-nowrap">
                    {f.name}
                    {f.required && (
                      <span className="text-bad" title="required">
                        {" "}
                        *
                      </span>
                    )}
                  </td>
                  <td className="py-1.5 pr-3 whitespace-nowrap text-ink-soft">
                    {f.type}
                    {f.scale ? `(${f.scale})` : ""}
                  </td>
                  <td className="mono py-1.5 pr-3 whitespace-nowrap text-ink-soft">
                    {f.encoding?.kind ?? "—"}
                  </td>
                  <td className="mono py-1.5 pr-3 whitespace-nowrap text-ink-soft">
                    {f.null_values?.length
                      ? f.null_values.map((v) => JSON.stringify(v)).join(" ")
                      : "—"}
                  </td>
                  <td className="py-1.5 text-ink-soft">
                    <Description text={f.description} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-3 max-w-3xl text-xs text-ink-soft/80">
          Type and encoding are separate on purpose: <span className="mono">overpunch_trailing</span>{" "}
          and <span className="mono">cyymmdd</span> are ways of spelling a decimal and a date, not
          types of their own. Legacy systems have no NULL, so which value means absent is recorded
          per field.
        </p>
      </Panel>
    </div>
  );
}
