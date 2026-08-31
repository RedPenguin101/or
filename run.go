package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file is one story told in order: what a Run *is*, what it takes to ask
// for one, and what happens when you do.
//
// A Run captures the versions of every input plus what came out. Because all
// four inputs are content-addressed, their digest is the Run's identity and
// re-executing it is guaranteed to reproduce this output (design.md §4).
type Run struct {
	Number     int       `json:"run"`
	Project    string    `json:"project"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Actor      Actor     `json:"actor"`

	// Reason is required, not optional metadata. The audit obligation is about
	// *why* a change was made, and a field that can be skipped will be.
	Reason string `json:"reason"`

	Inputs InputVersions `json:"inputs"`

	// Read names the dataset record each input was satisfied by, keyed by the
	// transform's input alias.
	//
	// Not part of InputVersions and deliberately outside the digest: an id is not
	// content, so two identical uploads have different ones, and folding an id in
	// would mean the same four inputs no longer produced the same digest. It is a
	// durable link, not a version — how many rows the ingest coerced and how many
	// it withheld belong to the Dataset, and are read from it rather than copied
	// here where they could only go stale or disagree.
	Read map[string]string `json:"read"`

	Nodes   map[string]NodeStat `json:"nodes"`
	Outputs []RuleOutput        `json:"outputs"`
	Totals  Totals              `json:"totals"`
}

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// InputVersions is the recorded identity of everything a Run consumed, in two
// halves, because the four inputs are two kinds of thing:
//
//	Mapping   what the source looks like, what the target looks like, how to map
//	          between them — authored, revised by decision
//	Data      the rows going through it — delivered, never edited
//
// Split rather than flat because "why do these two runs differ?" is then one
// comparison instead of four: same mapping and different data means the extract
// changed; same data and different mapping means we did.
//
// The Specs belong to the mapping half and were missing entirely until it was
// noticed that spec validation *withholds* payloads — a changed target contract
// changes the output, which makes it an input whether or not it was written down.
type InputVersions struct {
	Mapping MappingVersions `json:"mapping"`
	Data    DataVersions    `json:"data"`

	// Digest is the hash of both halves: the Run's identity. Two Runs with the
	// same digest are guaranteed to have produced the same output.
	//
	// A digest nested inside the record it describes necessarily covers the
	// fields beside it rather than itself. That is unlike a stored body, whose
	// hash is its key and must cover the whole thing.
	Digest string `json:"digest"`
}

type MappingVersions struct {
	Schemas   map[string]string `json:"schemas"`
	Transform string            `json:"transform"`
	Specs     map[string]string `json:"specs"`
	Digest    string            `json:"digest"`
}

type DataVersions struct {
	Datasets map[string]string `json:"datasets"` // input alias -> dataset content hash
	Digest   string            `json:"digest"`
}

type NodeStat struct {
	Hash   string `json:"hash"`
	Cached bool   `json:"cached"`
	Rows   int    `json:"rows_out"`
	Ms     int64  `json:"ms"`
}

// RuleOutput is what one Rule produced: the payloads it emitted to its target
// and everything it withheld on the way.
type RuleOutput struct {
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

// Totals is what the *rules* did: what they emitted, what the target accepted,
// what was withheld on the way.
//
// Rows that failed coercion are not counted here. They never reached a rule, so
// no rule is answerable for them, and folding them in made "emitted" describe
// rows that were never emitted. They belong to the ingest and are read from the
// Dataset, which is where they happened.
type Totals struct {
	Emitted  int `json:"emitted"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// RunRequest is everything one Run consumes, plus who asked for it and why.
//
// The four things it is a function of — what the source looks like, what the
// target looks like, how to map between them, and the data itself — are recorded
// as InputVersions on the Run, and their digest is the Run's identity.
type RunRequest struct {
	Number  int
	Project string
	Reason  string
	Actor   Actor

	// Mapping is the three authored inputs together: the source schemas, the
	// transform, and the target contract it writes against.
	Mapping *Mapping

	// Datasets is what satisfies each of the transform's declared inputs.
	// DatasetID carries the record id alongside, so the Run can point back at the
	// ingest without copying what that ingest already knows.
	Datasets  map[string]*Dataset // input alias -> dataset
	DatasetID map[string]string   // input alias -> dataset record id

}

// ExecuteRun is the pure function the whole system is built around:
//
//	Output = Run(SourceSchemas, TargetSpecs, Transform, SourceDatasets)
//
// Everything it consumes is content-addressed and everything it decides —
// including which identities to quarantine — is a function of those inputs, so
// the same digest always produces the same Run.
//
// The Engine and the store root are parameters rather than part of the request:
// one is the machine that executes and the other is where the bytes happen to
// sit. Neither is something the request is about, and neither is an input to the
// Run's identity.
func ExecuteRun(eng *Engine, store string, in RunRequest) (*Run, error) {
	started := time.Now()

	mv, err := in.Mapping.Versions()
	if err != nil {
		return nil, err
	}
	run := &Run{
		Number:    in.Number,
		Project:   in.Project,
		StartedAt: started,
		Actor:     in.Actor,
		Reason:    in.Reason,
		Read:      map[string]string{},
		Nodes:     map[string]NodeStat{},
		Inputs: InputVersions{
			Mapping: mv,
			Data:    DataVersions{Datasets: map[string]string{}},
		},
	}

	inputViews := map[string]bool{}
	for _, inp := range in.Mapping.Transform.Inputs {
		ds, ok := in.Datasets[inp.As]
		if !ok {
			return nil, fmt.Errorf("transform declares input %q, which no dataset satisfies", inp.As)
		}
		if ds.SchemaName != inp.SchemaName {
			return nil, fmt.Errorf("input %q: transform expects schema %s, dataset carries %s",
				inp.As, inp.SchemaName, ds.SchemaName)
		}
		// The mapping decides what it will accept, including that the data was
		// coerced against the schema version the mapping actually pins.
		if err := in.Mapping.Admits(ds); err != nil {
			return nil, fmt.Errorf("input %q: %w", inp.As, err)
		}
		if err := eng.Attach(inp.As, ds.DataPath(store)); err != nil {
			return nil, err
		}
		inputViews[inp.As] = true
		run.Inputs.Data.Datasets[inp.As] = ds.Hash
		run.Read[inp.As] = in.DatasetID[inp.As]
	}
	run.Inputs.Data.Digest = hashJSON(run.Inputs.Data.Datasets)
	run.Inputs.Digest = hashJSON([]string{run.Inputs.Mapping.Digest, run.Inputs.Data.Digest})

	// mintRef applies a rule's prefix to a raw source key. A reference never
	// restates the mint expression, so the two cannot drift apart.
	mintOf := map[string]string{}
	for _, r := range in.Mapping.Transform.Rules {
		mintOf[r.Name] = r.Mints.Prefix
	}
	mintRef := func(rule, raw string) string { return mintOf[rule] + raw }

	// quarantined holds full minted codes. Rules minting one entity are required
	// to have disjoint prefixes, so a code names exactly one target object and a
	// single flat set is enough.
	quarantined := map[string]RejectRef{}

	for _, rule := range in.Mapping.Transform.Ordered() {
		table := "rule_" + rule.Name

		t0 := time.Now()
		n, err := eng.Materialise(table, in.Mapping.Transform.SQL[rule.Name])
		if err != nil {
			return nil, err
		}
		// A node's hash folds in its inputs, so changing one rule invalidates
		// only what depends on it rather than the world (design.md §4).
		run.Nodes[rule.Name] = NodeStat{
			Hash:   hashJSON([]string{in.Mapping.Transform.SQL[rule.Name], run.Inputs.Digest}),
			Cached: false, Rows: n, Ms: time.Since(t0).Milliseconds(),
		}

		res := RuleOutput{Rule: rule.Name, Title: rule.Title, Target: rule.To}

		if in.Mapping.Spec != nil {
			if err := checkColumnsAgainstTarget(eng, in.Mapping.Spec, rule, table); err != nil {
				return nil, err
			}
		}
		if err := checkMintedPrefix(eng, rule, table); err != nil {
			return nil, err
		}

		rows, err := eng.Rows(table)
		if err != nil {
			return nil, err
		}
		ents, conflicts := Collapse(rows, rule)
		for _, c := range conflicts {
			res.Rejects = append(res.Rejects, c)
			quarantined[c.Identity] = RejectRef{Output: rule.Name, Identity: c.Identity}
		}

		compiled, err := eng.Query(rule.Binding.Compile(table))
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.Name, err)
		}
		// First row wins, matching Collapse, so the payload and the collapsed
		// entity always agree about which source row spoke for it.
		byID := map[string]map[string]any{}
		for _, r := range compiled {
			id, ok := r[rule.Binding.RowKey].(string)
			if !ok {
				continue
			}
			if _, seen := byID[id]; !seen {
				byID[id] = r
			}
		}

		for _, e := range ents {
			row, ok := byID[e.Identity]
			if !ok {
				continue
			}
			// Pre-emission quarantine: withhold anything naming a quarantined
			// identity rather than emitting it for Qniform to refuse. Doing the
			// cascade here keeps the error causally accurate — "withheld because
			// its rep conflicts" rather than "code does not resolve".
			if ref, why := referencesQuarantined(rule, row, quarantined, mintRef); why != "" {
				res.Rejects = append(res.Rejects, Reject{
					Class: RejectRow, Phase: PhaseValidation,
					Entity: rule.Mints.Entity, Identity: e.Identity,
					SourceRow: firstRow(e.Rows), Message: why, Cause: &ref,
				})
				quarantined[e.Identity] = RejectRef{Output: rule.Name, Identity: e.Identity}
				continue
			}
			res.Payloads = append(res.Payloads, rule.Binding.Build(row, mintRef))
		}

		// Check the batch against the target's own spec. A payload that fails
		// here is our rule's fault, not the extract's and not Qniform's.
		maxItems := 500
		if in.Mapping.Spec != nil && len(res.Payloads) > 0 {
			if doc, err := in.Mapping.Spec.Describe(rule.To); err == nil && doc.MaxItems > 0 {
				maxItems = doc.MaxItems
			}
			specRejects, err := in.Mapping.Spec.ValidateBatch(rule.To, res.Payloads)
			if err != nil {
				return nil, fmt.Errorf("rule %s: %w", rule.Name, err)
			}
			if len(specRejects) > 0 {
				bad := map[string]bool{}
				for _, r := range specRejects {
					bad[r.Identity] = true
				}
				kept := res.Payloads[:0]
				for _, p := range res.Payloads {
					id, _ := p.Get(rule.Binding.RowKey)
					if s, _ := id.(string); bad[s] {
						quarantined[s] = RejectRef{Output: rule.Name, Identity: s}
						continue
					}
					kept = append(kept, p)
				}
				res.Payloads = kept
				res.Rejects = append(res.Rejects, specRejects...)
			}
		}

		res.Accepted = len(res.Payloads)
		res.Rejected = len(res.Rejects)
		res.Emitted = res.Accepted + res.Rejected
		if res.Accepted > 0 {
			res.Requests = (res.Accepted + maxItems - 1) / maxItems
		}
		run.Outputs = append(run.Outputs, res)

		run.Totals.Emitted += res.Emitted
		run.Totals.Accepted += res.Accepted
		run.Totals.Rejected += res.Rejected
	}

	run.FinishedAt = time.Now()
	return run, nil
}

// checkColumnsAgainstTarget holds the line that there is no intermediate model:
// a rule produces the target's own fields, named as the target names them.
//
// Three kinds of column are legitimate. A field of the target endpoint, because
// that is what the model is for. A column a nesting key is read from, because
// that value becomes a key rather than a field and so has no target name. And a
// `__`-prefixed infrastructure column, which never leaves the system.
func checkColumnsAgainstTarget(eng *Engine, spec *Spec, rule Rule, table string) error {
	doc, err := spec.Describe(rule.To)
	if err != nil || doc.Item == nil {
		return nil // no shape to check against; ValidateBatch reports it later
	}
	cols, err := eng.columns(table)
	if err != nil {
		return err
	}
	used := rule.Binding.Columns()

	var stray []string
	for _, c := range cols {
		if strings.HasPrefix(c, "__") {
			continue
		}
		if _, isField := doc.Item.Properties[c]; isField {
			continue
		}
		if used[c] {
			continue // read for a nesting key, so it carries no target name
		}
		stray = append(stray, c)
	}
	if len(stray) > 0 {
		return fmt.Errorf(
			"rule %s: columns [%s] are neither fields of %s nor read by anything. "+
				"A rule produces the target's own fields, named as the target names them; "+
				"a column that is neither is an intermediate vocabulary, and those belong "+
				"nowhere. Either rename it to the field it becomes, or drop it",
			rule.Name, strings.Join(stray, ", "), rule.To)
	}
	return nil
}

// checkMintedPrefix verifies the SQL actually mints into the key space the rule
// declared. The declaration is what references, closures and diffs are built on,
// so a model that quietly mints something else would corrupt all three.
func checkMintedPrefix(eng *Engine, rule Rule, table string) error {
	col := rule.Mints.IdentityColumn()
	if rule.Mints.Prefix == "" {
		return nil // an empty prefix constrains nothing
	}
	bad, err := eng.Query(fmt.Sprintf(
		"SELECT %s AS id FROM %s WHERE %s IS NOT NULL AND NOT starts_with(%s, '%s') LIMIT 3",
		quoteIdent(col), quoteIdent(table), quoteIdent(col), quoteIdent(col), rule.Mints.Prefix))
	if err != nil {
		return fmt.Errorf("rule %s: checking minted identities: %w", rule.Name, err)
	}
	if len(bad) > 0 {
		var ids []string
		for _, r := range bad {
			s, _ := r["id"].(string)
			ids = append(ids, s)
		}
		return fmt.Errorf(
			"rule %s mints %s outside its declared key space %q (e.g. %s). "+
				"References, slice closures and run diffs are all built on the declared "+
				"mint, so the SQL and the declaration have to agree",
			rule.Name, col, rule.Mints.Prefix, strings.Join(ids, ", "))
	}
	return nil
}

// referencesQuarantined reports the first field on this row that names a
// withheld identity.
func referencesQuarantined(rule Rule, row map[string]any, q map[string]RejectRef,
	mintRef func(string, string) string) (RejectRef, string) {

	for _, name := range rule.Binding.fieldNames() {
		b := rule.Binding.Fields[name]
		if b.References == "" {
			continue
		}
		raw, _ := row[name].(string)
		if raw == "" {
			continue // a NULL column is an omitted key, so nothing dangles
		}
		id := mintRef(b.References, raw)
		if ref, bad := q[id]; bad {
			return ref, fmt.Sprintf("withheld: %s is quarantined, so %s cannot resolve", id, name)
		}
	}
	return RejectRef{}, ""
}

func firstRow(rows []int) int {
	if len(rows) == 0 {
		return 0
	}
	return rows[0]
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
// The work is driven entirely by the declaration in transform.yaml, which is the
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
			continue // quarantined: withheld, while the rest of the rule proceeds
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
