package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// Engine is the transformation compute: DuckDB, in process.
//
// It is deliberately used even at 20 rows. The point of the walking skeleton is
// the shape, and running the real engine from the first iteration is how the
// cost of cgo shows up in week one rather than month four (design.md §10, rule 6).
type Engine struct{ db *sql.DB }

func NewEngine() (*Engine, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Engine{db: db}, nil
}

func (e *Engine) Close() error { return e.db.Close() }

// sqlType maps a logical type to its DuckDB column type. The Dataset is typed —
// coercion has already happened, so carrying everything as text past this point
// would throw away the work and push casts into every rule.
func sqlType(f Field) string {
	switch f.Type {
	case TypeInteger:
		return "BIGINT"
	case TypeDecimal:
		return fmt.Sprintf("DECIMAL(18,%d)", f.Scale)
	case TypeDate:
		return "DATE"
	case TypeBoolean:
		return "BOOLEAN"
	default: // string, enum
		return "VARCHAR"
	}
}

// WriteDataset materialises the coerced rows as Parquet. That file IS the
// Dataset — the delivered CSV was only transport — so there is no staging step
// here and nothing between the two to name.
//
// DuckDB's own COPY does the writing, so no separate Parquet library enters the
// build.
func (e *Engine) WriteDataset(path string, schema *SourceSchema, rows [][]*string, srcRows []int) error {
	cols := make([]string, 0, len(schema.Fields)+1)
	for _, f := range schema.Fields {
		cols = append(cols, fmt.Sprintf("%s %s", quoteIdent(f.Name), sqlType(f)))
	}
	// __row carries the source line number through every model so a produced
	// value can always be traced back to the cell it came from.
	cols = append(cols, "__row INTEGER")

	tmp := "ingest_tmp"
	if _, err := e.db.Exec("DROP TABLE IF EXISTS " + tmp); err != nil {
		return err
	}
	if _, err := e.db.Exec(fmt.Sprintf("CREATE TABLE %s (%s)", tmp, strings.Join(cols, ", "))); err != nil {
		return err
	}

	if len(rows) > 0 {
		ph := make([]string, len(schema.Fields)+1)
		for i := range ph {
			ph[i] = "?"
		}
		stmt, err := e.db.Prepare(fmt.Sprintf("INSERT INTO %s VALUES (%s)", tmp, strings.Join(ph, ", ")))
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, row := range rows {
			args := make([]any, 0, len(row)+1)
			for _, v := range row {
				if v == nil {
					args = append(args, nil)
				} else {
					args = append(args, *v)
				}
			}
			args = append(args, srcRows[i])
			if _, err := stmt.Exec(args...); err != nil {
				return fmt.Errorf("staging row %d: %w", srcRows[i], err)
			}
		}
	}

	if _, err := e.db.Exec(fmt.Sprintf("COPY %s TO '%s' (FORMAT PARQUET)", tmp, path)); err != nil {
		return err
	}
	_, err := e.db.Exec("DROP TABLE " + tmp)
	return err
}

// Attach exposes a Dataset's Parquet file to the rules under a view name.
func (e *Engine) Attach(view, parquetPath string) error {
	_, err := e.db.Exec(fmt.Sprintf(
		"CREATE OR REPLACE VIEW %s AS SELECT * FROM read_parquet('%s')", quoteIdent(view), parquetPath))
	return err
}

// Materialise runs one model's SQL and holds the result as a table the next
// model can read. Models form a DAG; the caller walks it in order.
func (e *Engine) Materialise(name, query string) (int, error) {
	q := fmt.Sprintf("CREATE OR REPLACE TABLE %s AS %s", quoteIdent(name), query)
	if _, err := e.db.Exec(q); err != nil {
		return 0, fmt.Errorf("model %s: %w", name, err)
	}
	var n int
	if err := e.db.QueryRow("SELECT count(*) FROM " + quoteIdent(name)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Rows reads a materialised model back as ordered maps. Iteration-1 volumes only:
// at Sample or Full tier this becomes a streaming read.
func (e *Engine) Rows(name string) ([]map[string]any, error) {
	return e.Query("SELECT * FROM " + quoteIdent(name))
}

func (e *Engine) Query(q string) ([]map[string]any, error) {
	rows, err := e.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = normalise(vals[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// normalise flattens DuckDB's driver types into what the emitter and JSON need.
// Decimals in particular must not become floats: Qniform takes decimal strings
// and a float round-trip loses the scale the source declared.
func normalise(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		// DATE columns arrive as time.Time; render them the way the source
		// declared them and the target expects them, not as timestamps.
		return t.Format("2006-01-02")
	case fmt.Stringer:
		return t.String()
	default:
		return v
	}
}

func (e *Engine) columns(table string) ([]string, error) {
	rows, err := e.db.Query("SELECT * FROM " + quoteIdent(table) + " LIMIT 0")
	if err != nil {
		return nil, fmt.Errorf("reading columns of %s: %w", table, err)
	}
	defer rows.Close()
	return rows.Columns()
}

// quoteIdent lets source column names survive verbatim into SQL. Legacy names
// are hyphenated ("REP-ID") and the user must see their own column names in the
// rules, so they are quoted rather than mangled.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
