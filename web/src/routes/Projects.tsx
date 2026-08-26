import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { Empty, ErrorNote, Panel, Pill } from "../components/ui";

export default function Projects() {
  const qc = useQueryClient();
  const projects = useQuery({ queryKey: ["projects"], queryFn: api.projects });
  const types = useQuery({ queryKey: ["types"], queryFn: api.migrationTypes });

  const [name, setName] = useState("");
  const [type, setType] = useState("");

  const create = useMutation({
    mutationFn: () =>
      api.createProject({
        name,
        migration_type: type || types.data?.[0]?.name,
        actor: "joe",
      }),
    onSuccess: () => {
      setName("");
      qc.invalidateQueries({ queryKey: ["projects"] });
    },
  });

  return (
    <div className="space-y-5">
      <Panel title="New project">
        <p className="mb-3 max-w-2xl text-sm text-ink-soft">
          Choosing the Migration Type is what supplies the starting schemas and transformation
          rules — a new project knows the source system&apos;s tables before any data arrives.
        </p>
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create.mutate();
          }}
        >
          <label className="text-xs text-ink-soft">
            <div className="mb-1">Project name</div>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Northwind Securities"
              className="w-64 rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink"
            />
          </label>
          <label className="text-xs text-ink-soft">
            <div className="mb-1">Migration Type</div>
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              className="rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink"
            >
              {types.data?.map((t) => (
                <option key={t.name} value={t.name}>
                  {t.name} — {t.title}
                </option>
              ))}
            </select>
          </label>
          <button
            type="submit"
            disabled={!name.trim() || create.isPending}
            className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
          >
            {create.isPending ? "Creating…" : "Create"}
          </button>
        </form>
        {create.error && (
          <div className="mt-3">
            <ErrorNote error={create.error} />
          </div>
        )}
      </Panel>

      <Panel title="Projects">
        {projects.error && <ErrorNote error={projects.error} />}
        {projects.data?.length === 0 && <Empty>No projects yet.</Empty>}
        <ul className="divide-y divide-line">
          {projects.data?.map((p) => (
            <li key={p.id}>
              <Link
                to={`/p/${p.id}`}
                className="flex items-center gap-3 py-2.5 hover:text-accent"
              >
                <span className="text-sm font-medium">{p.name}</span>
                <Pill>{p.migration_type}</Pill>
                <span className="text-xs text-ink-soft">firm {p.firm}</span>
                <span className="ml-auto text-xs text-ink-soft">
                  {p.created_at.slice(0, 16)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}
