package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ pool *pgxpool.Pool }

func OpenDB(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", redactDSN(url), err)
	}
	return &DB{pool: pool}, nil
}

func (d *DB) Close() { d.pool.Close() }

func (d *DB) Migrate(ctx context.Context) error {
	b, err := os.ReadFile("migrations/001_initial_schema.sql")
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, string(b))
	return err
}

type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	MigrationType string `json:"migration_type"`
	Firm          string `json:"firm"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
}

func (d *DB) CreateProject(ctx context.Context, p Project) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO projects (id, name, migration_type, firm, created_by) VALUES ($1,$2,$3,$4,$5)`,
		p.ID, p.Name, p.MigrationType, p.Firm, p.CreatedBy)
	return err
}

func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, name, migration_type, firm, created_by, created_at::text
		   FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.MigrationType, &p.Firm, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := d.pool.QueryRow(ctx,
		`SELECT id, name, migration_type, firm, created_by, created_at::text
		   FROM projects WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.MigrationType, &p.Firm, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PutSchema records a version of one of a project's schemas.
//
// project_schemas is a write-only log: this appends, and never updates what it
// supersedes. The row it adds is the whole of the version — body, hash and the
// metadata saying who adopted it, when, and why.
func (d *DB) PutSchema(ctx context.Context, projectID, actor, reason string, s *SourceSchema) error {
	body, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx,
		`INSERT INTO project_schemas (project_id, source_system, schema_name, hash, body, created_by, reason)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		projectID, s.SourceSystem, s.Schema, s.Hash(), body, actor, reason)
	return err
}

// ListSchemas returns the project's schemas as they now stand: the newest row
// for each schema. Current is the last version written, never a row flagged as
// current — a flag would be a second place for the same fact to be recorded.
func (d *DB) ListSchemas(ctx context.Context, projectID string) ([]*SourceSchema, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT DISTINCT ON (source_system, schema_name) hash, body FROM project_schemas
		  WHERE project_id = $1 ORDER BY source_system, schema_name, id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SourceSchema{}
	for rows.Next() {
		var hash string
		var body []byte
		if err := rows.Scan(&hash, &body); err != nil {
			return nil, err
		}
		var s SourceSchema
		if err := json.Unmarshal(body, &s); err != nil {
			return nil, err
		}
		if got := s.Hash(); got != hash {
			return nil, fmt.Errorf("schema %s hashes to %s: stored body does not match its hash", hash, got)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// GetMapping returns the project's mapping as it now stands: its schemas, its
// transform, and the target contract it writes against.
//
// One call because they are one thing. Fetched separately, the three read as
// unrelated lookups a caller has to remember to make together — which is how the
// check that a dataset was coerced against the schema the mapping pins ended up
// belonging to nobody.
func (d *DB) GetMapping(ctx context.Context, projectID string) (*Mapping, error) {
	schemas, err := d.ListSchemas(ctx, projectID)
	if err != nil {
		return nil, err
	}
	x, err := d.GetTransform(ctx, projectID)
	if err != nil {
		return nil, err
	}
	spec, err := d.GetSpec(ctx, projectID, TargetSystem)
	if err != nil {
		return nil, err
	}
	m := &Mapping{Schemas: map[SchemaName]*SourceSchema{}, Transform: x, Spec: spec}
	for _, sc := range schemas {
		m.Schemas[sc.SchemaName] = sc
	}
	return m, nil
}

// PutTransform records a version of the project's transform. Appends; see
// PutSchema.
func (d *DB) PutTransform(ctx context.Context, projectID, actor, reason string, x *Transform) error {
	body, err := json.Marshal(x)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx,
		`INSERT INTO project_transforms (project_id, name, hash, body, created_by, reason)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		projectID, x.Name, x.Hash(), body, actor, reason)
	return err
}

// GetTransform returns the transform this project now runs.
//
// Iteration 1 holds exactly one. Nothing here falls back to the shipped
// catalogue: reading the working copy when a project has no version of its own
// is the failure this log exists to remove.
func (d *DB) GetTransform(ctx context.Context, projectID string) (*Transform, error) {
	var hash string
	var body []byte
	err := d.pool.QueryRow(ctx,
		`SELECT DISTINCT ON (name) hash, body FROM project_transforms
		  WHERE project_id = $1 ORDER BY name, id DESC LIMIT 1`, projectID).Scan(&hash, &body)
	if err != nil {
		return nil, fmt.Errorf("this project has no transform: %w", err)
	}
	x, err := ParseTransform(body)
	if err != nil {
		return nil, err
	}
	// The hash is what a Run records, so a body that no longer hashes to it is
	// corruption, not a difference of opinion. Checking costs nothing and is the
	// only thing that makes "content-addressed" a fact rather than an intention.
	if got := x.Hash(); got != hash {
		return nil, fmt.Errorf("transform %s hashes to %s: stored body does not match its hash", hash, got)
	}
	return x, nil
}

// PutSpec records a version of the target contract the project migrates onto.
// Appends; see PutSchema.
func (d *DB) PutSpec(ctx context.Context, projectID, actor, reason string, s *Spec) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO project_specs (project_id, target, hash, body, created_by, reason)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		projectID, TargetSystem, s.Hash(), string(s.Body()), actor, reason)
	return err
}

// GetSpec returns the target contract this project now migrates onto — the shape
// Qniform accepted when the project adopted it, not whatever it accepts today.
func (d *DB) GetSpec(ctx context.Context, projectID, target string) (*Spec, error) {
	var hash string
	var body []byte
	err := d.pool.QueryRow(ctx,
		`SELECT hash, body FROM project_specs
		  WHERE project_id = $1 AND target = $2 ORDER BY id DESC LIMIT 1`,
		projectID, target).Scan(&hash, &body)
	if err != nil {
		return nil, fmt.Errorf("this project has no %s spec: %w", target, err)
	}
	s, err := ParseSpec(body)
	if err != nil {
		return nil, err
	}
	if got := s.Hash(); got != hash {
		return nil, fmt.Errorf("spec %s hashes to %s: stored body does not match its hash", hash, got)
	}
	return s, nil
}

type DatasetRow struct {
	Dataset
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	CreatedAt string   `json:"created_at"`
	CreatedBy string   `json:"created_by"`
	Rejects   []Reject `json:"rejects"`
}

func (d *DB) PutDataset(ctx context.Context, r DatasetRow) error {
	rj, err := json.Marshal(r.Rejects)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx,
		`INSERT INTO datasets (id, project_id, source_system, schema_name, hash, schema_hash, rows, rejected, rejects, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.ProjectID, r.SourceSystem, r.Schema, r.Hash, r.SchemaHash,
		r.Rows, r.Rejected, rj, r.CreatedBy)
	return err
}

func (d *DB) ListDatasets(ctx context.Context, projectID string) ([]DatasetRow, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, project_id, source_system, schema_name, hash, schema_hash, rows, rejected, rejects, created_at::text, created_by
		   FROM datasets WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatasetRow{}
	for rows.Next() {
		var r DatasetRow
		var rj []byte
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.SourceSystem, &r.Schema, &r.Hash, &r.SchemaHash,
			&r.Rows, &r.Rejected, &rj, &r.CreatedAt, &r.CreatedBy); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rj, &r.Rejects)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) NextRunNumber(ctx context.Context, projectID string) (int, error) {
	var n int
	err := d.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM runs WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

func (d *DB) PutRun(ctx context.Context, id, projectID string, r *Run) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx,
		`INSERT INTO runs (id, project_id, number, digest, reason, actor, body, started_at, finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, projectID, r.Number, r.Inputs.Digest, r.Reason,
		r.Actor.Kind+":"+r.Actor.ID, body, r.StartedAt, r.FinishedAt)
	return err
}

type RunSummary struct {
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Digest    string `json:"digest"`
	Reason    string `json:"reason"`
	Actor     string `json:"actor"`
	StartedAt string `json:"started_at"`
	Emitted   int    `json:"emitted"`
	Accepted  int    `json:"accepted"`
	Rejected  int    `json:"rejected"`
}

func (d *DB) ListRuns(ctx context.Context, projectID string) ([]RunSummary, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, number, digest, reason, actor, started_at::text,
		        (body->'totals'->>'emitted')::int,
		        (body->'totals'->>'accepted')::int,
		        (body->'totals'->>'rejected')::int
		   FROM runs WHERE project_id = $1 ORDER BY number DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunSummary{}
	for rows.Next() {
		var s RunSummary
		if err := rows.Scan(&s.ID, &s.Number, &s.Digest, &s.Reason, &s.Actor,
			&s.StartedAt, &s.Emitted, &s.Accepted, &s.Rejected); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) GetRun(ctx context.Context, id string) (json.RawMessage, error) {
	var body json.RawMessage
	err := d.pool.QueryRow(ctx, `SELECT body FROM runs WHERE id = $1`, id).Scan(&body)
	return body, err
}

func redactDSN(url string) string {
	if i := indexOfByte(url, '@'); i > 0 {
		return "postgres://***" + url[i:]
	}
	return url
}

func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
