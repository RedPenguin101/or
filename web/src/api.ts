export type MigrationType = {
  name: string;
  title: string;
  schemas: string[];
  xform: string;
};

export type Project = {
  id: string;
  name: string;
  migration_type: string;
  firm: string;
  created_by: string;
  created_at: string;
};

export type Field = {
  name: string;
  type: string;
  required: boolean;
  description: string;
  encoding: { kind?: string };
  scale?: number;
  values?: Record<string, string>;
  null_values?: string[];
};

export type Schema = {
  source_system: string;
  schema: string;
  title: string;
  description: string;
  layout: string;
  fields: Field[];
  hash: string;
};

export type Reject = {
  class: "row" | "conflict";
  phase: "coercion" | "validation" | "target";
  entity: string;
  identity?: string;
  message: string;
  source_row?: number;
  field?: string;
  value?: string;
  values?: string[];
  source_rows?: number[];
  cause?: { output: string; identity: string };
};

export type Dataset = {
  id: string;
  name: string;
  schema_ref: string;
  hash: string;
  rows: number;
  rejected: number;
  rejects: Reject[];
  created_at: string;
  created_by: string;
};

export type Output = {
  name: string;
  title?: string;
  target: string;
  emitted: number;
  accepted: number;
  rejected: number;
  requests: number;
  payloads: Record<string, unknown>[] | null;
  rejects: Reject[] | null;
};

export type Run = {
  // Absent on records written before source counts were recorded. Readers must
  // tolerate every shape ever written: run records are audit artifacts and are
  // never migrated in place.
  record_version?: number;
  run: number;
  project: string;
  tier: string;
  reason: string;
  actor: { kind: string; id: string };
  started_at: string;
  finished_at: string;
  source?: { delivered: number; rows: number };
  inputs: {
    schemas: Record<string, string>;
    datasets: Record<string, string>;
    xform: string;
    triple: string;
  };
  nodes: Record<string, { hash: string; cached: boolean; rows_out: number; ms: number }>;
  outputs: Output[];
  withheld_at_ingest: Reject[];
  totals: { emitted: number; accepted: number; rejected: number };
};

// Run records on disk are never migrated — they are audit artifacts, and
// rewriting them would destroy the property they exist to provide. So every
// historical shape is adapted here, in one place, rather than by conditionals
// scattered through the components.
//
//   v0  no `source`; emissions under `stages`, with ingest among them
//   v1  `source` added
//   v2  emissions renamed `outputs`; ingest rejects promoted to
//       `withheld_at_ingest`; a reject's `stage` renamed `phase`
//   v3  `source.staged` renamed `source.rows` — the coerced table IS the
//       Dataset, so there is no separate staged thing to name
type RawRun = Omit<Run, "outputs" | "withheld_at_ingest" | "source"> & {
  source?: { delivered: number; rows?: number; staged?: number };
  outputs?: Output[];
  withheld_at_ingest?: Reject[];
  stages?: (Output & { rejects: (Reject & { stage?: Reject["phase"] })[] | null })[];
};

export function normalizeRun(raw: RawRun): Run {
  const version = raw.record_version ?? 0;

  const fixReject = (r: Reject & { stage?: Reject["phase"] }): Reject => ({
    ...r,
    phase: r.phase ?? r.stage ?? "validation",
    cause: r.cause
      ? { output: r.cause.output ?? (r.cause as { stage?: string }).stage ?? "", identity: r.cause.identity }
      : undefined,
  });

  const source = raw.source
    ? { delivered: raw.source.delivered, rows: raw.source.rows ?? raw.source.staged ?? 0 }
    : undefined;

  if (version >= 2) {
    return {
      ...raw,
      source,
      outputs: raw.outputs ?? [],
      withheld_at_ingest: raw.withheld_at_ingest ?? [],
    };
  }

  // Before v2 the coercion rejects rode along as a pseudo-emission called
  // "ingest". Lift them out so every consumer sees one shape.
  const all = raw.stages ?? [];
  const ingest = all.find((s) => s.name === "ingest");
  return {
    ...raw,
    source,
    outputs: all
      .filter((s) => s.name !== "ingest")
      .map((s) => ({ ...s, rejects: (s.rejects ?? []).map(fixReject) })),
    withheld_at_ingest: (ingest?.rejects ?? []).map(fixReject),
  };
}

export type RunSummary = {
  id: string;
  number: number;
  triple: string;
  status: string;
  reason: string;
  actor: string;
  started_at: string;
  emitted: number;
  accepted: number;
  rejected: number;
};

export type Identity = {
  column: string;
  derived_from: string[];
  conflict?: { on: string[]; resolve: string };
};

// A Rule binds source schemas to one Output. It is the unit a person reasons in,
// and the level at which the mapping should be legible without reading SQL.
export type Rule = {
  name: string;
  title: string;
  summary: string;
  from: string[];
  to: string;
  models: string[];
  key: string;
  output_title: string;
  target: string;
  entity: string;
  identity?: Identity;
  emitter: string;
};

export type XformOutput = {
  name: string;
  title: string;
  entity: string;
  emitter: string;
  target: string;
  after?: string[];
};

export type Emitter = {
  target: string;
  row_key: string;
  batch: { max_items: number; failure_mode: string };
  order: string[] | null;
  // No `from` and no `omit_when`: a model column carries the name of the field
  // it becomes, and absence is a NULL in the model. What is left cannot express
  // a business decision.
  fields: Record<string, { references?: string; map?: { key_from: string } }>;
};

export type FieldCoverage = {
  name: string;
  type: string;
  required: boolean;
  enum?: string[];
  description?: string;
  bound: boolean;
  references?: string;
  key_from?: string;
};

// An Output paired with what the target itself says it accepts. The join of the
// two — every field Qniform takes, marked with whether a rule binds it — is what
// turns "what have we not mapped yet" from folklore into a list.
export type OutputDetail = {
  name: string;
  title: string;
  target: string;
  entity: string;
  after: string[] | null;
  batch: { max_items: number; failure_mode: string };
  doc?: {
    target: string;
    summary: string;
    description: string;
    max_items: number;
  };
  doc_error?: string;
  fields: FieldCoverage[];
  rules: Rule[];
  unknown_fields: string[];
};

export type Xform = {
  name: string;
  version: number;
  description: string;
  hash: string;
  inputs: { schema: string; as: string }[];
  outputs: XformOutput[];
  rules: Rule[];
  entities: { name: string; model: string; identity: Identity }[];
  models: Record<string, string>;
  shared_models: string[] | null;
  model_deps: Record<string, string[]>;
  emitters: Record<string, Emitter>;
};

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, init);
  if (!r.ok) {
    let msg = `${r.status} ${r.statusText}`;
    try {
      const b = await r.json();
      if (b.error) msg = b.error;
    } catch {
      /* non-JSON error body */
    }
    throw new Error(msg);
  }
  return r.json();
}

export const api = {
  migrationTypes: () => req<MigrationType[]>("/api/migration-types"),
  projects: () => req<Project[]>("/api/projects"),
  project: (id: string) => req<Project>(`/api/projects/${id}`),
  createProject: (body: unknown) =>
    req<Project>("/api/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
  schemas: (id: string) => req<Schema[]>(`/api/projects/${id}/schemas`),
  xform: (id: string) => req<Xform>(`/api/projects/${id}/xform`),
  output: (id: string, name: string) =>
    req<OutputDetail>(`/api/projects/${id}/outputs/${name}`),
  datasets: (id: string) => req<Dataset[]>(`/api/projects/${id}/datasets`),
  datasetRows: (id: string, did: string) =>
    req<{ dataset: Dataset; rows: Record<string, unknown>[] }>(
      `/api/projects/${id}/datasets/${did}/rows`,
    ),
  uploadDataset: (id: string, form: FormData) =>
    req<Dataset>(`/api/projects/${id}/datasets`, { method: "POST", body: form }),
  runs: (id: string) => req<RunSummary[]>(`/api/projects/${id}/runs`),
  run: (rid: string) => req<RawRun>(`/api/runs/${rid}`).then(normalizeRun),
  createRun: (id: string, body: unknown) =>
    req<{ id: string; run: RawRun }>(`/api/projects/${id}/runs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => ({ id: r.id, run: normalizeRun(r.run) })),
};
