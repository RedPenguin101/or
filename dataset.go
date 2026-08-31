package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A Dataset is an uploaded body of data keyed to a Source Schema.
//
// The raw bytes are kept exactly as delivered and never rewritten: the landed
// file is the evidence, and schema-on-read means a schema correction is a
// metadata edit rather than a re-upload (devnotes/design.md §3, Layer 1).
// A Dataset is identified by the schema it belongs to and the hash of what was
// delivered. There is deliberately no file name: the delivered file is transport
// and nothing else, so naming it here would put a label in front of users that
// means nothing to the work and cannot identify anything — two deliveries can
// share a name, and one file can be delivered twice.
type Dataset struct {
	SchemaName
	Hash Hash `json:"hash"` // content hash of the raw bytes: the Dataset's version

	// SchemaHash is the version of the schema these bytes were coerced against.
	// Rows and Rejected are a function of it, so without it neither number can
	// be explained — the same file against a revised null sentinel is a
	// different Dataset.
	SchemaHash Hash `json:"schema_hash"`

	Rows     int `json:"rows"`     // rows that coerced cleanly and landed
	Rejected int `json:"rejected"` // rows withheld at coercion
}

// Where a Dataset's files live. One directory per content hash, written once and
// never rewritten.
//
// Derived from the hash, never stored. A stored path is a second source of truth
// that cannot be re-derived once it is wrong: the Parquet file was renamed from
// staged.parquet to rows.parquet and every row already written kept pointing at
// a name that no longer existed. The hash is the only durable part, so the path
// is computed from it at the moment of use.
const (
	rawFile  = "raw.csv"
	dataFile = "rows.parquet"
)

func datasetDir(store string, hash Hash) string {
	return filepath.Join(store, "datasets", strings.ReplaceAll(string(hash), ":", "_"))
}

// RawPath is the delivered bytes, exactly as they arrived: the landed evidence.
func (d *Dataset) RawPath(store string) string {
	return filepath.Join(datasetDir(store, d.Hash), rawFile)
}

// DataPath is the Parquet of the typed rows — the Dataset itself.
func (d *Dataset) DataPath(store string) string {
	return filepath.Join(datasetDir(store, d.Hash), dataFile)
}

// Ingest reads a delivered file against its schema, lands the raw bytes, coerces
// every cell, and writes the typed rows to Parquet. Rows with any coercion
// failure are withheld and returned as rejects rather than landed half-read.
func Ingest(eng *Engine, store string, schema *SourceSchema, raw []byte) (*Dataset, []Reject, error) {
	hash := hashBytes(raw)

	dir := datasetDir(store, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, rawFile), raw, 0o644); err != nil {
		return nil, nil, err
	}

	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
	}

	// A column the schema does not know about is a schema problem, not a row
	// problem — it means the extract changed shape, which the user must see.
	var unknown []string
	for _, h := range header {
		if _, ok := schema.Field(h); !ok {
			unknown = append(unknown, h)
		}
	}
	if len(unknown) > 0 {
		return nil, nil, fmt.Errorf("columns not in schema %s: %s", schema.SchemaName, strings.Join(unknown, ", "))
	}
	for _, f := range schema.Fields {
		if f.Required && !contains(header, f.Name) {
			return nil, nil, fmt.Errorf("required field %q missing from the file", f.Name)
		}
	}

	var (
		rejects []Reject
		typed   [][]*string
		srcRows []int
	)
	line := 1 // header consumed
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", line+1, err)
		}
		line++

		row := make([]*string, len(schema.Fields))
		var errs []string
		for i, f := range schema.Fields {
			idx := indexOf(header, f.Name)
			var cell string
			if idx >= 0 && idx < len(rec) {
				cell = rec[idx]
			}
			v, cerr := f.Coerce(cell)
			if cerr != nil {
				errs = append(errs, cerr.Error())
				continue
			}
			if v == nil && f.Required {
				errs = append(errs, fmt.Sprintf("%s: required field is absent", f.Name))
				continue
			}
			row[i] = v
		}
		// Report every fault on the row at once. Fixing one bad cell only to be
		// shown the next one is the slowest possible way to clean an extract.
		if len(errs) > 0 {
			rejects = append(rejects, Reject{
				Class:     RejectRow,
				Phase:     PhaseCoercion,
				Entity:    schema.String(),
				SourceRow: line,
				Message:   strings.Join(errs, "; "),
			})
			continue
		}
		typed = append(typed, row)
		srcRows = append(srcRows, line)
	}

	if err := eng.WriteDataset(filepath.Join(dir, dataFile), schema, typed, srcRows); err != nil {
		return nil, nil, err
	}

	return &Dataset{
		SchemaName: schema.SchemaName,
		Hash:       hash,
		SchemaHash: schema.Hash(),
		Rows:       len(typed),
		Rejected:   len(rejects),
	}, rejects, nil
}

func contains(ss []string, s string) bool { return indexOf(ss, s) >= 0 }

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}
