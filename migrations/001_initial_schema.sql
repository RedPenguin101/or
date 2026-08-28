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

-- Source Schemas are content-addressed: the hash IS the version, so a project
-- referencing a hash is pinned to exactly the schema a Run used.
CREATE TABLE IF NOT EXISTS source_schemas (
    hash        TEXT PRIMARY KEY,
    ref         TEXT        NOT NULL,
    body        JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_schemas (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ref         TEXT NOT NULL,
    schema_hash TEXT NOT NULL REFERENCES source_schemas(hash),
    PRIMARY KEY (project_id, ref)
);

CREATE TABLE IF NOT EXISTS datasets (
    id          TEXT PRIMARY KEY,
    project_id  TEXT        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    schema_ref  TEXT        NOT NULL,
    hash        TEXT        NOT NULL,   -- content hash of the delivered bytes
    raw_path    TEXT        NOT NULL,   -- never rewritten: the landed evidence
    stage_path  TEXT        NOT NULL,
    rows        INTEGER     NOT NULL,
    rejected    INTEGER     NOT NULL,
    rejects     JSONB       NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT        NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    id           TEXT PRIMARY KEY,
    project_id   TEXT        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number       INTEGER     NOT NULL,
    -- The Run's identity: hash of (schemas, datasets, xform). Two runs with the
    -- same triple are guaranteed to have produced the same output.
    triple       TEXT        NOT NULL,
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

-- Existing databases were created with JSONB. Guarded so the rewrite happens
-- once rather than on every boot.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'body' AND data_type = 'jsonb'
    ) THEN
        ALTER TABLE runs ALTER COLUMN body TYPE JSON USING body::text::json;
    END IF;
END $$;
