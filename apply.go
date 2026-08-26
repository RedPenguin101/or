package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Apply is the pure function the whole system is built around:
//
//	Output = Apply(SourceSchemas, SourceDatasets, Xforms)
//
// Everything it consumes is content-addressed and everything it decides —
// including which entities to quarantine — is a function of those inputs, so the
// same triple always produces the same Run.
type ApplyInput struct {
	Project  string
	Tier     string
	Reason   string
	Actor    Actor
	Firm     string
	Schemas  map[string]*SourceSchema // input alias -> schema
	Datasets map[string]*Dataset      // input alias -> dataset
	Xform    *Xform
	Prior    []Reject // rejects carried in from ingest (coercion)

	// Spec is Qniform's OpenAPI document. When present every emitted batch is
	// checked against it before the Run is reported, so a shape error surfaces
	// here rather than as a 400 from the target.
	Spec *Spec
}

func Apply(eng *Engine, in ApplyInput, number int) (*Run, error) {
	started := time.Now()

	run := &Run{
		RecordVersion: CurrentRecordVersion,

		Number:    number,
		Project:   in.Project,
		Tier:      in.Tier,
		StartedAt: started,
		Actor:     in.Actor,
		Reason:    in.Reason,
		Nodes:     map[string]NodeStat{},
		Target:    RunTarget{Kind: "preview", Firm: in.Firm},
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

	// --- models, in dependency order -------------------------------------------
	order, deps, err := in.Xform.ModelOrder(inputViews)
	if err != nil {
		return nil, err
	}
	nodeHash := map[string]string{}
	for _, name := range order {
		// A node's hash folds in its inputs' hashes, so changing one model
		// invalidates only its downstream cone rather than the world (design.md §4).
		parts := []string{in.Xform.Models[name]}
		for _, d := range deps[name] {
			parts = append(parts, nodeHash[d])
		}
		parts = append(parts, run.Inputs.Triple)
		nodeHash[name] = hashJSON(parts)

		t0 := time.Now()
		n, err := eng.Materialise(name, in.Xform.Models[name])
		if err != nil {
			return nil, err
		}
		run.Nodes[name] = NodeStat{
			Hash: nodeHash[name], Cached: false, Rows: n,
			Ms: time.Since(t0).Milliseconds(),
		}
	}

	// --- build each entity, then collapse it on its declared identity -----------
	//
	// An entity has no authored model: it is the union of what its Rules produce.
	// Generating it means every model belongs to a rule, so no rule logic can
	// hide in an unclaimed "preparation" model the summary never shows.
	entities := map[string][]entityRow{}
	conflicts := map[string][]Reject{}
	for _, e := range in.Xform.Entities {
		models := in.Xform.RuleModels(e.Name)
		if len(models) == 0 {
			return nil, fmt.Errorf("entity %q is produced by no rule", e.Name)
		}
		if err := eng.UnionInto(EntityTable(e.Name), models); err != nil {
			return nil, err
		}
		rows, err := eng.Rows(EntityTable(e.Name))
		if err != nil {
			return nil, err
		}
		ents, confs := Collapse(rows, e)
		entities[e.Name] = ents
		conflicts[e.Name] = confs
	}

	// --- stages, in declared order, with the quarantine cascade -----------------
	//
	// quarantined is keyed entity -> identity. Because stages run in order and
	// each stage adds its own withholdings, the cascade is transitive: a party
	// withheld for a name conflict withholds the accounts naming it, which would
	// in turn withhold anything naming those.
	quarantined := map[string]map[string]RejectRef{}
	mark := func(entity, id string, ref RejectRef) {
		if quarantined[entity] == nil {
			quarantined[entity] = map[string]RejectRef{}
		}
		quarantined[entity][id] = ref
	}

	for _, st := range orderOutputs(in.Xform.Outputs) {
		ent, _ := in.Xform.Entity(st.Entity)
		em := in.Xform.Emitters[st.Emitter]

		res := OutputResult{Name: st.Name, Title: st.Title, Target: st.Target}

		for _, c := range conflicts[st.Entity] {
			res.Rejects = append(res.Rejects, c)
			mark(st.Entity, c.Identity, RejectRef{Output: st.Name, Identity: c.Identity})
		}

		// Every column a rule produces must be a field of the target endpoint,
		// a column a nesting key is read from, or infrastructure. Without this
		// check a rule can quietly grow a vocabulary of its own between source
		// and target — which is what a "staging" model turned out to be.
		if in.Spec != nil {
			if err := checkColumnsAgainstTarget(eng, in.Spec, st, em, ent.Name); err != nil {
				return nil, err
			}
		}

		compiled, err := eng.Query(em.Compile(EntityTable(ent.Name)))
		if err != nil {
			return nil, fmt.Errorf("output %s: %w", st.Name, err)
		}
		// First row wins, matching Collapse's choice, so the emitted payload and
		// the collapsed entity always agree about which source row spoke for it.
		byID := map[string]map[string]any{}
		for _, r := range compiled {
			id, ok := r[em.RowKey].(string)
			if !ok {
				continue
			}
			if _, seen := byID[id]; !seen {
				byID[id] = r
			}
		}

		for _, e := range entities[st.Entity] {
			row, ok := byID[e.Identity]
			if !ok {
				continue
			}

			// Pre-emission quarantine: withhold anything naming a quarantined
			// identity rather than emitting it for Qniform to refuse. Doing the
			// cascade here is what keeps the error causally accurate — "withheld
			// because its rep conflicts" rather than "code does not resolve".
			if ref, why := referencesQuarantined(em, row, quarantined); why != "" {
				res.Rejects = append(res.Rejects, Reject{
					Class:     RejectRow,
					Phase:     PhaseValidation,
					Entity:    st.Entity,
					Identity:  e.Identity,
					SourceRow: firstRow(e.Rows),
					Message:   why,
					Cause:     &ref,
				})
				mark(st.Entity, e.Identity, RejectRef{Output: st.Name, Identity: e.Identity})
				continue
			}

			res.Payloads = append(res.Payloads, em.Payload(row))
		}

		// Check the batch against the target's own spec. A payload that fails
		// here is our rule's fault, not the extract's and not Qniform's, which
		// is why it lands at stage "validation".
		if in.Spec != nil && len(res.Payloads) > 0 {
			specRejects, err := in.Spec.ValidateBatch(st.Target, res.Payloads)
			if err != nil {
				return nil, fmt.Errorf("output %s: %w", st.Name, err)
			}
			if len(specRejects) > 0 {
				bad := map[string]bool{}
				for _, r := range specRejects {
					bad[r.Identity] = true
				}
				kept := res.Payloads[:0]
				for _, p := range res.Payloads {
					id, _ := p.Get("code")
					if s, _ := id.(string); bad[s] {
						mark(st.Entity, s, RejectRef{Output: st.Name, Identity: s})
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
			res.Requests = (res.Accepted + em.Batch.MaxItems - 1) / em.Batch.MaxItems
		}
		run.Outputs = append(run.Outputs, res)

		run.Totals.Emitted += res.Emitted
		run.Totals.Accepted += res.Accepted
		run.Totals.Rejected += res.Rejected
	}

	// Coercion rejects belong to the Run, not to any Output: these rows failed
	// before a rule saw them, so no output is answerable for them. They used to
	// be carried as a pseudo-output, which made every consumer filter it out.
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
func checkColumnsAgainstTarget(eng *Engine, spec *Spec, out Output, em *Emitter, entity string) error {
	doc, err := spec.Describe(out.Target)
	if err != nil || doc.Item == nil {
		return nil // no shape to check against; ValidateBatch reports it later
	}
	cols, err := eng.columns(EntityTable(entity))
	if err != nil {
		return err
	}
	used := em.UsedColumns()

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
			"output %s: rule columns [%s] are neither fields of %s nor read by anything. "+
				"A rule produces the target's own fields, named as the target names them; "+
				"a column that is neither is an intermediate vocabulary, and those belong "+
				"nowhere. Either rename it to the field it becomes, or drop it",
			out.Name, strings.Join(stray, ", "), out.Target)
	}
	return nil
}

// referencesQuarantined reports the first field on this row that names a
// withheld identity.
func referencesQuarantined(em *Emitter, row map[string]any, q map[string]map[string]RejectRef) (RejectRef, string) {
	for _, name := range em.fieldNames() {
		b := em.Fields[name]
		if b.References == "" {
			continue
		}
		// A NULL column is an omitted key, so there is nothing to dangle.
		id, _ := row[name].(string)
		if id == "" {
			continue
		}
		if ref, bad := q[b.References][id]; bad {
			return ref, fmt.Sprintf("withheld: %s %s is quarantined, so %s cannot resolve",
				b.References, id, name)
		}
	}
	return RejectRef{}, ""
}

// orderOutputs sorts by the declared `after` edges. The ordering is a property of
// the target rather than of the project: accounts cannot load before the parties
// they name, on every migration.
func orderOutputs(outputs []Output) []Output {
	idx := map[string]Output{}
	var names []string
	for _, s := range outputs {
		idx[s.Name] = s
		names = append(names, s.Name)
	}
	sort.Strings(names)

	var out []Output
	done := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if done[n] {
			return
		}
		done[n] = true
		for _, a := range idx[n].After {
			visit(a)
		}
		out = append(out, idx[n])
	}
	for _, n := range names {
		visit(n)
	}
	return out
}

func firstRow(rows []int) int {
	if len(rows) == 0 {
		return 0
	}
	return rows[0]
}
