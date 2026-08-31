export type MigrationType = {
  name: string;
  title: string;
  schemas: SchemaName[];
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

// A content hash, e.g. "sha256:a6088e2b58c1". An alias, so it is documentation
// rather than a check — TypeScript is structural, and a branded type would cost
// a cast at every boundary for a value that only ever arrives from the server.
// It still stops Record<string, string> from saying nothing about either half.
export type Hash = string;

// A Source Schema's identity, in its two parts. The backend never joins them
// into one string, and neither should this.
export type SchemaName = {
  source_system: string;
  schema: string;
};

export const schemaKey = (n: SchemaName) => `${n.source_system}/${n.schema}`;
export const sameSchema = (a: SchemaName, b: SchemaName) =>
  a.source_system === b.source_system && a.schema === b.schema;
export const schemaPath = (projectId: string, n: SchemaName) =>
  `/p/${projectId}/s/${encodeURIComponent(n.source_system)}/${encodeURIComponent(n.schema)}`;

export type Schema = SchemaName & {
  title: string;
  description: string;
  layout: string;
  fields: Field[];
  hash: Hash;
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

export type Dataset = SchemaName & {
  id: string;
  hash: Hash;
  schema_hash: Hash;
  rows: number;
  rejected: number;
  rejects: Reject[];
  created_at: string;
  created_by: string;
};

// A Run's status is a reading of its totals, not a fact the backend stores:
// nothing server-side branches on it, so deriving it in the one place it is
// displayed beats keeping a column and a second implementation that can drift.
//
// Quarantine is not failure. A run that withholds some rows and emits the rest
// is partial, and its output is still reproducible and still diffable.
export type RunStatus = "clean" | "partial" | "blocked";

export function runStatus(t: { accepted: number; rejected: number }): RunStatus {
  if (t.rejected === 0) return "clean";
  return t.accepted === 0 ? "blocked" : "partial";
}

export const statusTone = (s: RunStatus) =>
  s === "clean" ? "good" : s === "blocked" ? "bad" : "warn";

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
  run: number;
  project: string;
  reason: string;
  actor: { kind: string; id: string };
  started_at: string;
  finished_at: string;
  // Dataset record id per input alias — the link back to the ingest.
  read: Record<string, string>;
  // Two halves, because the four inputs are two kinds of thing: the mapping is
  // authored and revised by decision, the data is delivered. Separate digests
  // answer "did the mapping change, or the data?" in one comparison.
  inputs: {
    mapping: {
      schemas: Record<string, Hash>;
      transform: Hash;
      specs: Record<string, Hash>;
      digest: Hash;
    };
    data: {
      datasets: Record<string, Hash>;
      digest: Hash;
    };
    digest: Hash;
  };
  nodes: Record<string, { hash: Hash; cached: boolean; rows_out: number; ms: number }>;
  outputs: Output[];
  totals: { emitted: number; accepted: number; rejected: number };
};

export type RunSummary = {
  id: string;
  number: number;
  digest: Hash;
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
  from: SchemaName[];
  column?: string;
  conflict?: { on: string[]; resolve: string };
};

export type Bind = {
  references?: string;
  key_from?: string;
  object?: Record<string, unknown>;
};

export type TargetBinding = {
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
  from: SchemaName[];
  to: string;
  mints: Mint;
  binding: TargetBinding;
};

// SQL is keyed by rule name: a rule's SQL is sql/<name>.sql, by convention
// rather than declaration, so the two cannot disagree.
export type Transform = {
  name: string;
  version: number;
  description: string;
  hash: Hash;
  inputs: { schema: string; as: string }[];
  targets: string[];
  rules: Rule[];
  sql: Record<string, string>;
  sql_reads: Record<string, string[]>;
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
  transform: (id: string) => req<Transform>(`/api/projects/${id}/transform`),
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
  run: (rid: string) => req<Run>(`/api/runs/${rid}`),
  createRun: (id: string, body: unknown) =>
    req<{ id: string; run: Run }>(`/api/projects/${id}/runs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
};
