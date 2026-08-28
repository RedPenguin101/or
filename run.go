package main

import (
	"fmt"
	"sort"
	"time"
)

// A Run captures the versions of every input plus what came out. Because the
// three inputs are content-addressed, the triple below is the Run's identity and
// re-executing it is guaranteed to reproduce this output (design.md §4).
type Run struct {
	// RecordVersion identifies the shape of this record, so a reader years from
	// now can branch on it explicitly rather than probing for fields.
	//
	// A Run is an audit artifact: it is written once and read by code that has
	// long since moved on. Older records are never migrated — rewriting history
	// is precisely what the audit trail exists to prevent — so every reader must
	// tolerate the shapes that came before. Absent means 0, the shape written
	// before source counts were recorded.
	RecordVersion int `json:"record_version"`

	Number     int       `json:"run"`
	Project    string    `json:"project"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Actor      Actor     `json:"actor"`

	// Reason is required, not optional metadata. The audit obligation is about
	// *why* a change was made, and a field that can be skipped will be.
	Reason string `json:"reason"`

	Inputs  RunInputs           `json:"inputs"`
	Source  Source              `json:"source"`
	Nodes   map[string]NodeStat `json:"nodes"`
	Target  RunTarget           `json:"target"`
	Outputs []OutputResult      `json:"outputs"`
	Totals  Totals              `json:"totals"`

	// WithheldAtIngest belongs to the Run rather than to any Output: these rows
	// failed coercion before a rule ever saw them, so no output is answerable
	// for them.
	WithheldAtIngest []Reject `json:"withheld_at_ingest"`
}

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type RunInputs struct {
	Schemas  map[string]string `json:"schemas"`
	Datasets map[string]string `json:"datasets"`
	Xform    string            `json:"xform"`
	Triple   string            `json:"triple"`
}

// CurrentRecordVersion is stamped on new Runs. Bump it whenever the record gains
// or changes a field, and note the difference where readers branch.
const CurrentRecordVersion = 4

// Source is how much data the Run actually read. It is recorded rather than
// inferred from an Output's counts: derivation means no Output's row count
// stands for the source's, and a fan-out of 20 rows into 22 parties is exactly
// the case where guessing goes wrong.
type Source struct {
	// Delivered is how many rows arrived in the file. Rows is how many are in
	// the Dataset — the CSV is only transport, and the coerced, typed table on
	// the far side of ingest IS the Dataset. They differ by whatever could not
	// be read.
	Delivered int `json:"delivered"`
	Rows      int `json:"rows"`
}

type NodeStat struct {
	Hash   string `json:"hash"`
	Cached bool   `json:"cached"`
	Rows   int    `json:"rows_out"`
	Ms     int64  `json:"ms"`
}

type RunTarget struct {
	Kind          string `json:"kind"`
	QniformSchema string `json:"qniform_schema,omitempty"`
	Firm          string `json:"firm,omitempty"`
}

// OutputResult is what one Output produced. "Output" is the domain term for a
// target API call that receives transformed data; the code used to call these
// stages, which collided with a Reject's phase and read as implementation.
type OutputResult struct {
	Rule     string        `json:"rule"`
	Title    string        `json:"title"`
	Target   string        `json:"target"`
	Emitted  int           `json:"emitted"`
	Accepted int           `json:"accepted"`
	Rejected int           `json:"rejected"`
	Requests int           `json:"requests"`
	Payloads []*OrderedMap `json:"payloads"`
	Rejects  []Reject      `json:"rejects"`
}

type Totals struct {
	Emitted  int `json:"emitted"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// Status is deliberately not binary. A Run that quarantines is a *partial* run,
// not a failed one: quarantine is a pure function of the inputs, so the output is
// still reproducible and still diffable against another Run.
func (r *Run) Status() string {
	switch {
	case r.Totals.Accepted == 0 && r.Totals.Rejected > 0:
		return "blocked"
	case r.Totals.Rejected > 0:
		return "partial"
	default:
		return "clean"
	}
}

// entityRow is one collapsed entity: the values it carries and the source rows
// that produced it.
type entityRow struct {
	Identity string
	Values   map[string]any
	Rows     []int
}

// Collapse groups a model's output on the declared identity column and checks the
// declared conflict columns.
//
// The work is driven entirely by the declaration in xform.yaml, which is the
// point of declaring identity separately from minting it: one implementation
// detects every entity's conflicts rather than each rule hand-rolling its own.
func Collapse(rows []map[string]any, rule Rule) ([]entityRow, []Reject) {
	idCol := rule.Mints.IdentityColumn()
	groups := map[string][]map[string]any{}
	var order []string
	for _, r := range rows {
		id, _ := r[idCol].(string)
		if id == "" {
			continue
		}
		if _, seen := groups[id]; !seen {
			order = append(order, id)
		}
		groups[id] = append(groups[id], r)
	}
	sort.Strings(order) // deterministic: the output must not depend on scan order

	var (
		out       []entityRow
		conflicts []Reject
	)
	for _, id := range order {
		g := groups[id]
		// Sort by source row so the representative row is chosen the same way
		// every time, and the same way the emitter chooses it.
		sort.SliceStable(g, func(i, j int) bool {
			a, _ := toInt(g[i]["__row"])
			b, _ := toInt(g[j]["__row"])
			return a < b
		})

		srcRows := map[int]bool{}
		for _, r := range g {
			if n, ok := toInt(r["__row"]); ok {
				srcRows[n] = true
			}
		}
		rowList := make([]int, 0, len(srcRows))
		for n := range srcRows {
			rowList = append(rowList, n)
		}
		sort.Ints(rowList)

		conflicted := false
		if c := rule.Mints.Conflict; c != nil {
			for _, col := range c.On {
				vals := distinctStrings(g, col)
				if len(vals) <= 1 {
					continue
				}
				conflicted = true
				conflicts = append(conflicts, Reject{
					Class:      RejectConflict,
					Phase:      PhaseValidation,
					Entity:     rule.Mints.Entity,
					Identity:   id,
					Field:      col,
					Values:     vals,
					SourceRows: rowList,
					Message: fmt.Sprintf("%d source rows derive %s %s with %d distinct values for %s",
						len(rowList), rule.Mints.Entity, id, len(vals), col),
				})
			}
		}
		if conflicted {
			continue // quarantined: withheld, while the rest of the stage proceeds
		}

		vals := map[string]any{}
		for k, v := range g[0] {
			vals[k] = v
		}
		vals["__rows"] = rowList
		out = append(out, entityRow{Identity: id, Values: vals, Rows: rowList})
	}
	return out, conflicts
}

func distinctStrings(rows []map[string]any, col string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		s, ok := r[col].(string)
		if !ok || s == "" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
