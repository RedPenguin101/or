package main

import "fmt"

// A Mapping is the description of a transformation between two data formats:
// what the source looks like, what the target looks like, and how to get from
// one to the other. Three of a Run's four inputs, and the three that are
// *authored* — seeded from a Migration Type's catalogue, then revised through
// the application, each revision a decision someone had to give a reason for.
//
// The fourth input, the Datasets, is a different kind of thing entirely: rows
// that arrive from the client. It is delivered rather than decided, has no seed
// and no reason, and is never edited. Keeping the two apart is what lets a Run
// answer the question a migrator actually asks when two runs differ — was it the
// mapping that changed, or the data?
//
// A Mapping holds no data and executes nothing. The Engine and the store are
// parameters to ExecuteRun, not members here.
type Mapping struct {
	Schemas   map[SchemaName]*SourceSchema `json:"schemas"`
	Transform *Transform                   `json:"transform"`
	Spec      *Spec                        `json:"-"`
}

// Admits reports whether a Dataset can be run through this Mapping.
//
// Matching by schema *name* is not enough, and that gap was a live bug: a
// revised schema left old datasets still matching by name, so a Run paired rows
// typed under the previous version with the version it recorded as its input.
// Two runs could then share a digest and produce different output, which is the
// one invariant the whole design rests on.
func (m *Mapping) Admits(ds *Dataset) error {
	sc, ok := m.Schemas[ds.SchemaName]
	if !ok {
		return fmt.Errorf("this mapping has no schema %s", ds.SchemaName)
	}
	if want := sc.Hash(); ds.SchemaHash != want {
		return fmt.Errorf(
			"dataset was coerced against %s %s, but the mapping now uses %s. "+
				"Row types, null sentinels and what was withheld all follow the schema, "+
				"so the data has to be re-ingested against the current version before it "+
				"can be run through this mapping",
			ds.SchemaName, ds.SchemaHash, want)
	}
	return nil
}

// Versions is the Mapping's recorded identity: a hash per part, and a digest
// over all of them.
func (m *Mapping) Versions() (MappingVersions, error) {
	v := MappingVersions{
		Schemas:   map[string]Hash{},
		Specs:     map[string]Hash{},
		Transform: m.Transform.Hash(),
	}
	for name, sc := range m.Schemas {
		v.Schemas[name.String()] = sc.Hash()
	}
	// Only the endpoints this transform writes. The rest of the target's contract
	// is not an input to a Run that never touches it.
	if m.Spec != nil {
		for _, target := range m.Transform.Targets() {
			h, err := m.Spec.HashFor(target)
			if err != nil {
				return v, fmt.Errorf("recording the contract for %s: %w", target, err)
			}
			v.Specs[target] = h
		}
	}
	v.Digest = hashJSON(struct {
		Schemas   map[string]Hash
		Transform Hash
		Specs     map[string]Hash
	}{v.Schemas, v.Transform, v.Specs})
	return v, nil
}
