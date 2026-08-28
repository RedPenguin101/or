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
  rule: string;
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
//   v4  emissions are per-rule (`rule` rather than `name`), since a rule now
//       owns its whole chain; `tier` dropped
type RawRun = Omit<Run, "outputs" | "withheld_at_ingest" | "source"> & {
  source?: { delivered: number; rows?: number; staged?: number };
  outputs?: Output[];
  withheld_at_ingest?: Reject[];
  stages?: (Output & { name?: string; rejects: (Reject & { stage?: Reject["phase"] })[] | null })[];
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
      .map((s) => ({
        ...s,
        // Before v4 an emission was named for its endpoint, not its rule.
        rule: s.rule ?? s.name ?? "",
        rejects: (s.rejects ?? []).map(fixReject),
      })),
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

// A Mint declares how a rule constructs the identities it produces. Declared
// rather than left in SQL because identity does far more work than the
// expression suggests: dedupe, conflict detection, reference resolution, the
// quarantine cascade, aligning one run against another for diffing, and the
// closure a slice has to follow.
export type Mint = {
  entity: string;
  prefix: string;
  from: string[];
  column?: string;
  conflict?: { on: string[]; resolve: string };
};

export type Bind = {
  references?: string;
  key_from?: string;
  object?: Record<string, unknown>;
};

export type PayloadSpec = {
  row_key: string;
  fields: Record<string, Bind>;
  order: string[] | null;
};

// A Rule is the whole chain: the source it reads, the identity it mints, the SQL
// that flattens it, and the shape of the payload that leaves.
export type Rule = {
  name: string;
  title: string;
  summary: string;
  from: string[];
  to: string;
  model: string;
  mints: Mint;
  payload: PayloadSpec;
};

export type Xform = {
  name: string;
  version: number;
  description: string;
  hash: string;
  inputs: { schema: string; as: string }[];
  targets: string[];
  rules: Rule[];
  models: Record<string, string>;
  model_deps: Record<string, string[]>;
};

export type FieldCoverage = {
  name: string;
  type: string;
  required: boolean;
  enum?: string[];
  description?: string;
  bound: string[];
  references?: string;
  key_from?: string;
  literal?: boolean;
};

// A target endpoint paired with what it accepts and which rules populate it.
// Several rules can write one endpoint, so coverage is a union across them.
export type TargetDetail = {
  target: string;
  load_rank: number;
  doc?: { target: string; summary: string; description: string; max_items: number };
  doc_error?: string;
  fields: FieldCoverage[];
  rules: Rule[];
  unknown_fields: string[];
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
  target: (id: string, target: string) =>
    req<TargetDetail>(`/api/projects/${id}/target?t=${encodeURIComponent(target)}`),
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
