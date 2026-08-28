package main

import (
	"fmt"
	"strings"
	"time"
)

// Apply is the pure function the whole system is built around:
//
//	Output = Apply(SourceSchemas, SourceDatasets, Xforms)
//
// Everything it consumes is content-addressed and everything it decides —
// including which identities to quarantine — is a function of those inputs, so
// the same triple always produces the same Run.
type ApplyInput struct {
	Project  string
	Reason   string
	Actor    Actor
	Firm     string
	Schemas  map[string]*SourceSchema // input alias -> schema
	Datasets map[string]*Dataset      // input alias -> dataset
	Xform    *Xform
	Prior    []Reject // rejects carried in from ingest (coercion)

	// Spec is Qniform's OpenAPI document. When present every payload is checked
	// against it before the Run is reported, so a shape error surfaces here
	// rather than as a 400 from the target.
	Spec *Spec
}

func Apply(eng *Engine, in ApplyInput, number int) (*Run, error) {
	started := time.Now()

	run := &Run{
		RecordVersion: CurrentRecordVersion,
		Number:        number,
		Project:       in.Project,
		StartedAt:     started,
		Actor:         in.Actor,
		Reason:        in.Reason,
		Nodes:         map[string]NodeStat{},
		Target:        RunTarget{Kind: "preview", Firm: in.Firm},
		Inputs: RunInputs{
			Schemas:  map[string]string{},
			Datasets: map[string]string{},
			Xform:    in.Xform.Hash(),
		},
	}

	inputViews := map[string]bool{}
	for _, inp := range in.Xform.Inputs {
		ds, ok := in.Datasets[inp.As]
		if !ok {
			return nil, fmt.Errorf("xform declares input %q, which no dataset satisfies", inp.As)
		}
		sc, ok := in.Schemas[inp.As]
		if !ok {
			return nil, fmt.Errorf("no schema for input %q", inp.As)
		}
		if sc.Ref() != inp.Schema {
			return nil, fmt.Errorf("input %q: xform expects schema %s, dataset carries %s",
				inp.As, inp.Schema, sc.Ref())
		}
		if err := eng.Attach(inp.As, ds.StagePath); err != nil {
			return nil, err
		}
		inputViews[inp.As] = true
		run.Inputs.Schemas[sc.Ref()] = sc.Hash()
		run.Inputs.Datasets[inp.As] = ds.Hash
		run.Source.Rows += ds.Rows
		run.Source.Delivered += ds.Rows + ds.Rejected
	}
	run.Inputs.Triple = hashJSON(run.Inputs)

	// mintRef applies a rule's prefix to a raw source key. A reference never
	// restates the mint expression, so the two cannot drift apart.
	mintOf := map[string]string{}
	for _, r := range in.Xform.Rules {
		mintOf[r.Name] = r.Mints.Prefix
	}
	mintRef := func(rule, raw string) string { return mintOf[rule] + raw }

	// quarantined holds full minted codes. Rules minting one entity are required
	// to have disjoint prefixes, so a code names exactly one target object and a
	// single flat set is enough.
	quarantined := map[string]RejectRef{}

	for _, rule := range in.Xform.Ordered() {
		table := "rule_" + rule.Name

		t0 := time.Now()
		n, err := eng.Materialise(table, in.Xform.Models[rule.Model])
		if err != nil {
			return nil, err
		}
		// A node's hash folds in its inputs, so changing one rule invalidates
		// only what depends on it rather than the world (design.md §4).
		run.Nodes[rule.Name] = NodeStat{
			Hash:   hashJSON([]string{in.Xform.Models[rule.Model], run.Inputs.Triple}),
			Cached: false, Rows: n, Ms: time.Since(t0).Milliseconds(),
		}

		res := OutputResult{Rule: rule.Name, Title: rule.Title, Target: rule.To}

		if in.Spec != nil {
			if err := checkColumnsAgainstTarget(eng, in.Spec, rule, table); err != nil {
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

		compiled, err := eng.Query(rule.Payload.Compile(table))
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.Name, err)
		}
		// First row wins, matching Collapse, so the payload and the collapsed
		// entity always agree about which source row spoke for it.
		byID := map[string]map[string]any{}
		for _, r := range compiled {
			id, ok := r[rule.Payload.RowKey].(string)
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
			res.Payloads = append(res.Payloads, rule.Payload.Payload(row, mintRef))
		}

		// Check the batch against the target's own spec. A payload that fails
		// here is our rule's fault, not the extract's and not Qniform's.
		maxItems := 500
		if in.Spec != nil && len(res.Payloads) > 0 {
			if doc, err := in.Spec.Describe(rule.To); err == nil && doc.MaxItems > 0 {
				maxItems = doc.MaxItems
			}
			specRejects, err := in.Spec.ValidateBatch(rule.To, res.Payloads)
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
					id, _ := p.Get(rule.Payload.RowKey)
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

	// Coercion rejects belong to the Run, not to any rule: these rows failed
	// before a rule saw them, so no rule is answerable for them.
	run.WithheldAtIngest = in.Prior
	if run.WithheldAtIngest == nil {
		run.WithheldAtIngest = []Reject{}
	}
	run.Totals.Emitted += len(in.Prior)
	run.Totals.Rejected += len(in.Prior)

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
	used := rule.Payload.Columns()

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

	for _, name := range rule.Payload.fieldNames() {
		b := rule.Payload.Fields[name]
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
