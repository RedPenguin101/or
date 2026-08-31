-- OnRamp metadata. Edited in place, pre-production.
--
-- This holds only metadata: dataset *content* lives as Parquet on the filesystem
-- (object storage later) and is executed by DuckDB. Postgres is small,
-- relational and transactional, which is what the project/schema/run graph
-- needs and what columnar scanning does not. See devnotes/design.md §5.

CREATE TABLE IF NOT EXISTS projects (
    id             TEXT PRIMARY KEY,
    name           TEXT        NOT NULL,
    migration_type TEXT        NOT NULL,
    firm           TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     TEXT        NOT NULL
);

-- The three authored inputs to a Run -- schemas, transform, target spec -- are
-- always the project's own. Nothing is shared between projects: what a project
-- runs is what its own rows say, and another project revising its copy is not
-- visible from here. (Folding a project's changes back into the shipped
-- catalogue, or a catalogue change forward into a running project, is a
-- deliberate act; there is no path by which either happens implicitly.)
--
-- Each table is a write-only log. The first row is copied from the seed
-- catalogue at project creation; every revision appends another, carrying its
-- own body, hash and metadata. Nothing is ever updated or deleted: the current
-- version is the newest row for a (project, name), and the rows it supersedes
-- are the audit trail -- who changed it, when, and why, which is the part that
-- still has to be explicable six months later.
--
-- Body and metadata live in one row on purpose. Splitting the bodies into a
-- content-addressed table of their own would deduplicate them across projects
-- and across reverts, but that is a storage saving of a few MB bought at the
-- price of the one thing this model says is true -- that a project shares
-- nothing with another project.
--
-- Bodies are JSON, never JSONB: a content-addressed body has to come back
-- exactly as it went in, and JSONB reserves the right to normalise it.

CREATE TABLE IF NOT EXISTS project_schemas (
    -- GENERATED ALWAYS, so nothing can choose its own place in the history:
    -- an explicit id is rejected rather than accepted and ordered by.
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- The schema's name, in its two natural parts: "FAS", "ACCOUNT-MASTER".
    -- Together they say which schema's history this row extends.
    source_system TEXT        NOT NULL,
    schema_name   TEXT        NOT NULL,
    hash          TEXT        NOT NULL,
    body          JSON        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT        NOT NULL,
    -- Required, as a Run's is. The audit obligation is about *why*.
    reason        TEXT        NOT NULL CHECK (length(trim(reason)) > 0)
);

CREATE TABLE IF NOT EXISTS project_transforms (
    -- GENERATED ALWAYS, so nothing can choose its own place in the history:
    -- an explicit id is rejected rather than accepted and ordered by.
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    hash       TEXT        NOT NULL,
    body       JSON        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT        NOT NULL,
    reason     TEXT        NOT NULL CHECK (length(trim(reason)) > 0)
);

-- The spec body is the document verbatim, as authored: YAML, because that is
-- what Qniform emits, and reformatting it would change a hash that is supposed
-- to mean "the same contract".
CREATE TABLE IF NOT EXISTS project_specs (
    -- GENERATED ALWAYS, so nothing can choose its own place in the history:
    -- an explicit id is rejected rather than accepted and ordered by.
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    target     TEXT        NOT NULL,
    hash       TEXT        NOT NULL,
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT        NOT NULL,
    reason     TEXT        NOT NULL CHECK (length(trim(reason)) > 0)
);

CREATE INDEX IF NOT EXISTS project_schemas_current    ON project_schemas    (project_id, source_system, schema_name, id DESC);
CREATE INDEX IF NOT EXISTS project_transforms_current ON project_transforms (project_id, name, id DESC);
CREATE INDEX IF NOT EXISTS project_specs_current      ON project_specs      (project_id, target, id DESC);

CREATE TABLE IF NOT EXISTS datasets (
    id            TEXT PRIMARY KEY,
    project_id    TEXT        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- Which schema this data was validated against, in the same two parts
    -- project_schemas names it by. There is no file name: the delivered file is
    -- transport, and a Dataset is identified by its schema and its content.
    source_system TEXT        NOT NULL,
    schema_name   TEXT        NOT NULL,
    -- The content hash of the delivered bytes. Where those bytes and the Parquet
    -- derived from them sit under ONRAMP_STORE is computed from this hash, never
    -- stored: a stored path cannot be re-derived once it is wrong, and renaming
    -- the Parquet file left every row here pointing at a name that had gone.
    hash          TEXT        NOT NULL,
    rows          INTEGER     NOT NULL,
    rejected      INTEGER     NOT NULL,
    rejects       JSONB       NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    TEXT        NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    id           TEXT PRIMARY KEY,
    project_id   TEXT        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number       INTEGER     NOT NULL,
    -- The Run's identity: the digest of what the source looks like, what the
    -- target looks like, how to map between them, and the data itself. Two runs
    -- with the same digest are guaranteed to have produced the same output.
    digest       TEXT        NOT NULL,
    status       TEXT        NOT NULL,
    -- Required, not optional. The audit obligation is about *why*.
    reason       TEXT        NOT NULL CHECK (length(trim(reason)) > 0),
    actor        TEXT        NOT NULL,
    -- JSON, not JSONB. JSONB normalises object key order (by key length, then
    -- bytewise), which silently discards the field order the payload spec
    -- declares — and that order is most of what makes a run diff readable.
    body         JSON        NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, number)
);

CREATE INDEX IF NOT EXISTS runs_project_number ON runs (project_id, number DESC);
CREATE INDEX IF NOT EXISTS datasets_project     ON datasets (project_id, created_at DESC);
