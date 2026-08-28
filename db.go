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

// PutSchema stores a schema under its content hash and pins the project to it.
// Re-storing the same schema is a no-op: identical content is the same version.
func (d *DB) PutSchema(ctx context.Context, projectID string, s *SourceSchema) error {
	body, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO source_schemas (hash, ref, body) VALUES ($1,$2,$3)
		 ON CONFLICT (hash) DO NOTHING`, s.Hash(), s.Ref(), body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO project_schemas (project_id, ref, schema_hash) VALUES ($1,$2,$3)
		 ON CONFLICT (project_id, ref) DO UPDATE SET schema_hash = EXCLUDED.schema_hash`,
		projectID, s.Ref(), s.Hash()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) ListSchemas(ctx context.Context, projectID string) ([]*SourceSchema, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT ss.body FROM project_schemas ps
		   JOIN source_schemas ss ON ss.hash = ps.schema_hash
		  WHERE ps.project_id = $1 ORDER BY ps.ref`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SourceSchema{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var s SourceSchema
		if err := json.Unmarshal(body, &s); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
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
		`INSERT INTO datasets (id, project_id, name, schema_ref, hash, raw_path, data_path, rows, rejected, rejects, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.ID, r.ProjectID, r.Name, r.SchemaRef, r.Hash, r.RawPath, r.DataPath,
		r.Rows, r.Rejected, rj, r.CreatedBy)
	return err
}

func (d *DB) ListDatasets(ctx context.Context, projectID string) ([]DatasetRow, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, project_id, name, schema_ref, hash, raw_path, data_path, rows, rejected, rejects, created_at::text, created_by
		   FROM datasets WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatasetRow{}
	for rows.Next() {
		var r DatasetRow
		var rj []byte
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.SchemaRef, &r.Hash, &r.RawPath,
			&r.DataPath, &r.Rows, &r.Rejected, &rj, &r.CreatedAt, &r.CreatedBy); err != nil {
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
		`INSERT INTO runs (id, project_id, number, digest, status, reason, actor, body, started_at, finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, projectID, r.Number, r.Inputs.Digest, r.Status(), r.Reason,
		r.Actor.Kind+":"+r.Actor.ID, body, r.StartedAt, r.FinishedAt)
	return err
}

type RunSummary struct {
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Digest    string `json:"digest"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Actor     string `json:"actor"`
	StartedAt string `json:"started_at"`
	Emitted   int    `json:"emitted"`
	Accepted  int    `json:"accepted"`
	Rejected  int    `json:"rejected"`
}

func (d *DB) ListRuns(ctx context.Context, projectID string) ([]RunSummary, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, number, digest, status, reason, actor, started_at::text,
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
		if err := rows.Scan(&s.ID, &s.Number, &s.Digest, &s.Status, &s.Reason, &s.Actor,
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
