package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// An Xform is the third input to Apply(). It is a directory, not a file: models
// are SQL and want to be SQL — diffable, highlighted, and readable by the
// client's own data people (design.md §3).
type Xform struct {
	Name        string   `yaml:"xform"       json:"name"`
	Version     int      `yaml:"version"     json:"version"`
	Description string   `yaml:"description" json:"description"`
	Inputs      []Input  `yaml:"inputs"      json:"inputs"`
	Entities    []Entity `yaml:"entities"    json:"entities"`
	Outputs     []Output `yaml:"outputs"     json:"outputs"`
	Rules       []Rule   `yaml:"rules"       json:"rules"`

	Models   map[string]string   `yaml:"-" json:"models"`   // name -> SQL
	Emitters map[string]*Emitter `yaml:"-" json:"emitters"` // path -> parsed
	Dir      string              `yaml:"-" json:"-"`
}

// Input is declared as a list even when it holds one entry, so nothing can grow
// a 1:1 dataset-to-endpoint assumption by accident (design.md §10, rule 3).
type Input struct {
	Schema string `yaml:"schema" json:"schema"`
	As     string `yaml:"as"     json:"as"`
}

// An Entity is a derived thing the target needs that the source has no table for.
// Minting is plain SQL inside the model; what is *declared* here is that a column
// IS an identity — which is what makes determinism checkable and set-level
// conflicts detectable generically (design.md §3a.1, iteration1-artifacts.md §2.2).
type Entity struct {
	Name     string   `yaml:"name"     json:"name"`
	Identity Identity `yaml:"identity" json:"identity"`
}

// EntityTable is where an entity's rows are materialised. An entity has no
// authored model: it is the union of whatever its Rules produce, so the table is
// built rather than written.
func EntityTable(entity string) string { return "entity_" + entity }

// RuleModels returns the models producing an entity, in rule declaration order.
func (x *Xform) RuleModels(entity string) []string {
	var out []string
	for _, o := range x.Outputs {
		if o.Entity != entity {
			continue
		}
		for _, r := range x.Rules {
			if r.To == o.Name {
				out = append(out, r.Models...)
			}
		}
	}
	return out
}

type Identity struct {
	Column      string    `yaml:"column"       json:"column"`
	DerivedFrom []string  `yaml:"derived_from" json:"derived_from"`
	Conflict    *Conflict `yaml:"conflict"     json:"conflict,omitempty"`
}

// Conflict says what to do when one identity is derived from rows that disagree.
// `error` quarantines the entity and lets the rest of the stage through.
type Conflict struct {
	On      []string `yaml:"on"      json:"on"`
	Resolve string   `yaml:"resolve" json:"resolve"` // error | most_common | first_by
}

// An Output is one target API call that receives transformed data. This is the
// domain term; the code called these "stages" and the UI called them
// "emissions", which read as implementation and collided with a Reject's phase.
type Output struct {
	Name    string   `yaml:"name"    json:"name"`
	Title   string   `yaml:"title"   json:"title"`
	Entity  string   `yaml:"entity"  json:"entity"`
	Emitter string   `yaml:"emitter" json:"emitter"`
	Target  string   `yaml:"target"  json:"target"`
	After   []string `yaml:"after"   json:"after"`
}

// A Rule is a named group of transformation logic binding source schemas to one
// Output. It is the unit a person reasons in — "the customer derivation" — and
// the level at which the mapping from source to target should be legible without
// reading any SQL.
//
// Rules are *declared* rather than inferred because the valuable part is the
// prose: what this group does and why it is not the obvious thing. No analysis
// of the SQL can produce that. Models a Rule does not claim are shared
// preparation, and are implementation detail at this level.
type Rule struct {
	Name    string   `yaml:"name"    json:"name"`
	Title   string   `yaml:"title"   json:"title"`
	Summary string   `yaml:"summary" json:"summary"`
	From    []string `yaml:"from"    json:"from"`   // Source Schema refs
	To      string   `yaml:"to"      json:"to"`     // Output name
	Models  []string `yaml:"models"  json:"models"` // implementation, shown on drill-down
	Key     string   `yaml:"key"     json:"key"`    // how identity is minted, in prose
}

func LoadXform(dir string) (*Xform, error) {
	b, err := os.ReadFile(filepath.Join(dir, "xform.yaml"))
	if err != nil {
		return nil, err
	}
	var x Xform
	if err := yaml.Unmarshal(b, &x); err != nil {
		return nil, fmt.Errorf("xform.yaml: %w", err)
	}
	x.Dir = dir
	x.Models = map[string]string{}
	x.Emitters = map[string]*Emitter{}

	sqls, _ := filepath.Glob(filepath.Join(dir, "models", "*.sql"))
	for _, p := range sqls {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		x.Models[strings.TrimSuffix(filepath.Base(p), ".sql")] = string(src)
	}
	for _, st := range x.Outputs {
		if _, ok := x.Emitters[st.Emitter]; ok {
			continue
		}
		em, err := LoadEmitter(filepath.Join(dir, st.Emitter))
		if err != nil {
			return nil, err
		}
		x.Emitters[st.Emitter] = em
	}
	if err := x.Validate(); err != nil {
		return nil, err
	}
	return &x, nil
}

func (x *Xform) Validate() error {
	if x.Name == "" {
		return fmt.Errorf("xform needs a name")
	}
	byEntity := map[string]bool{}
	for _, e := range x.Entities {
		if e.Identity.Column == "" {
			return fmt.Errorf("entity %q has no identity column", e.Name)
		}
		byEntity[e.Name] = true
	}
	byOutput := map[string]bool{}
	for _, o := range x.Outputs {
		if !byEntity[o.Entity] {
			return fmt.Errorf("output %q names unknown entity %q", o.Name, o.Entity)
		}
		byOutput[o.Name] = true
	}
	// Every model belongs to exactly one Rule. Without this an unclaimed model
	// becomes a place for rule logic to hide behind the label "implementation",
	// which is how a rule page ends up showing a thin projection while the real
	// derivation sits somewhere the summary never mentions.
	claimedBy := map[string]string{}
	for _, r := range x.Rules {
		if !byOutput[r.To] {
			return fmt.Errorf("rule %q produces unknown output %q", r.Name, r.To)
		}
		if len(r.Models) == 0 {
			return fmt.Errorf("rule %q names no models", r.Name)
		}
		for _, m := range r.Models {
			if _, ok := x.Models[m]; !ok {
				return fmt.Errorf("rule %q names model %q, which has no .sql file", r.Name, m)
			}
			if prev, dup := claimedBy[m]; dup {
				return fmt.Errorf("model %q is claimed by both rule %q and rule %q", m, prev, r.Name)
			}
			claimedBy[m] = r.Name
		}
	}
	for m := range x.Models {
		if _, ok := claimedBy[m]; !ok {
			return fmt.Errorf("model %q belongs to no rule; every model must belong to exactly one", m)
		}
	}
	return nil
}

func (x *Xform) Entity(name string) (Entity, bool) {
	for _, e := range x.Entities {
		if e.Name == name {
			return e, true
		}
	}
	return Entity{}, false
}

// Hash is the Xform's version: the manifest, every model's SQL, and every
// emitter, together. Changing one model changes the Xform, which is what makes
// a Run's input triple honest.
func (x *Xform) Hash() string {
	type flat struct {
		Manifest any
		Models   map[string]string
		Emitters map[string]*Emitter
	}
	m := map[string]any{
		"name": x.Name, "version": x.Version,
		"inputs": x.Inputs, "entities": x.Entities,
		"outputs": x.Outputs, "rules": x.Rules,
	}
	return hashJSON(flat{Manifest: m, Models: x.Models, Emitters: x.Emitters})
}

var identRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+"?([a-z_][a-z0-9_]*)"?`)

// ModelOrder topologically sorts models by scanning each one's SQL for the
// names of other models and of the attached input views.
//
// Inference rather than declaration is deliberate at model level: the names are
// already known, so a second place to restate them would only drift. Column-level
// dependencies are the harder problem and are the subject of the §7 lineage spike.
func (x *Xform) ModelOrder(inputs map[string]bool) ([]string, map[string][]string, error) {
	deps := map[string][]string{}
	for name, sql := range x.Models {
		seen := map[string]bool{}
		for _, m := range identRe.FindAllStringSubmatch(sql, -1) {
			ref := m[1]
			if ref == name || seen[ref] {
				continue
			}
			if _, isModel := x.Models[ref]; isModel {
				seen[ref] = true
				deps[name] = append(deps[name], ref)
			} else if inputs[ref] {
				seen[ref] = true
			}
		}
		sort.Strings(deps[name])
	}

	var order []string
	state := map[string]int{} // 0 unvisited, 1 in progress, 2 done
	var visit func(string, []string) error
	visit = func(n string, path []string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("model dependency cycle: %s", strings.Join(append(path, n), " -> "))
		case 2:
			return nil
		}
		state[n] = 1
		for _, d := range deps[n] {
			if err := visit(d, append(path, n)); err != nil {
				return err
			}
		}
		state[n] = 2
		order = append(order, n)
		return nil
	}
	names := make([]string, 0, len(x.Models))
	for n := range x.Models {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic order for models with no relative dependency
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, nil, err
		}
	}
	return order, deps, nil
}
