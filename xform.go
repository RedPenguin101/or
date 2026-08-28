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
	Name        string  `yaml:"xform"       json:"name"`
	Version     int     `yaml:"version"     json:"version"`
	Description string  `yaml:"description" json:"description"`
	Inputs      []Input `yaml:"inputs"      json:"inputs"`
	Rules       []Rule  `yaml:"rules"       json:"rules"`

	Models map[string]string `yaml:"-" json:"models"` // name -> SQL
	Dir    string            `yaml:"-" json:"-"`
}

// Input is declared as a list even when it holds one entry, so nothing can grow
// a 1:1 dataset-to-endpoint assumption by accident (design.md §10, rule 3).
type Input struct {
	Schema string `yaml:"schema" json:"schema"`
	As     string `yaml:"as"     json:"as"`
}

// A Rule is the whole chain from source to target: which schemas it reads, what
// identity it mints, the SQL that flattens it, and the shape of the payload that
// leaves. Nothing downstream renames, reinterprets or reshapes what it produces.
//
// Rules are declared rather than inferred because the valuable part is the
// prose: what this one does and where it is not the obvious thing.
type Rule struct {
	Name    string   `yaml:"name"    json:"name"`
	Title   string   `yaml:"title"   json:"title"`
	Summary string   `yaml:"summary" json:"summary"`
	From    []string `yaml:"from"    json:"from"`  // Source Schema refs
	To      string   `yaml:"to"      json:"to"`    // target endpoint, "PUT /parties"
	Model   string   `yaml:"model"   json:"model"` // the .sql that flattens it

	Mints   Mint        `yaml:"mints"   json:"mints"`
	Payload PayloadSpec `yaml:"payload" json:"payload"`
}

// Mint declares how a rule constructs the identities it produces.
//
// Declared rather than left in SQL because identity does far more work than the
// expression suggests: it deduplicates, detects disagreement, resolves
// references, drives the quarantine cascade, aligns one Run's output against
// another's for diffing, and — once slices exist — is what a closure walk
// follows. Every one of those needs to know the mint without executing it.
type Mint struct {
	// Entity is the kind of thing produced. It is a name, not a declared type:
	// what a target object *is* belongs to Qniform's spec, not to OnRamp, so
	// there is no second definition here to drift from it.
	Entity string `yaml:"entity" json:"entity"`

	// Prefix keeps each rule's identities in their own key space. Two rules
	// minting the same entity must have disjoint prefixes, which makes a
	// collision impossible by construction rather than detected after the fact.
	Prefix string `yaml:"prefix" json:"prefix"`

	// From is the source columns the identity is built from — the closure a
	// slice has to follow to bring an entity's dependencies along with it.
	From []string `yaml:"from" json:"from"`

	// Column is where the minted value lands in the model. Defaults to "code".
	Column string `yaml:"column" json:"column"`

	Conflict *Conflict `yaml:"conflict" json:"conflict,omitempty"`
}

// Conflict says what to do when one identity is minted from rows that disagree.
// `error` quarantines the entity and lets the rest of the rule through.
type Conflict struct {
	On      []string `yaml:"on"      json:"on"`
	Resolve string   `yaml:"resolve" json:"resolve"` // error | most_common | first_by
}

func (m Mint) IdentityColumn() string {
	if m.Column != "" {
		return m.Column
	}
	return "code"
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

	sqls, _ := filepath.Glob(filepath.Join(dir, "models", "*.sql"))
	for _, p := range sqls {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		x.Models[strings.TrimSuffix(filepath.Base(p), ".sql")] = string(src)
	}
	for i := range x.Rules {
		x.Rules[i].Payload.normalise()
	}
	// yaml.v3 loses map order, so recover each payload's authored field order
	// from the raw node: an emitted payload should read the way it was written.
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err == nil {
		for i, order := range payloadFieldOrder(&doc) {
			if i < len(x.Rules) {
				x.Rules[i].Payload.SetOrder(order)
			}
		}
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
	if len(x.Rules) == 0 {
		return fmt.Errorf("xform has no rules")
	}

	byName := map[string]bool{}
	claimed := map[string]string{}
	// prefixes[entity] = rule -> prefix, for the disjointness check below.
	prefixes := map[string]map[string]string{}

	for _, r := range x.Rules {
		if r.Name == "" {
			return fmt.Errorf("a rule has no name")
		}
		if byName[r.Name] {
			return fmt.Errorf("duplicate rule %q", r.Name)
		}
		byName[r.Name] = true

		if _, err := loadRank(r.To); err != nil {
			return fmt.Errorf("rule %q: %w", r.Name, err)
		}
		if _, ok := x.Models[r.Model]; !ok {
			return fmt.Errorf("rule %q names model %q, which has no .sql file", r.Name, r.Model)
		}
		// Every model belongs to exactly one rule. An unclaimed model is where
		// rule logic hides behind the label "implementation" while the rule page
		// shows a thin projection.
		if prev, dup := claimed[r.Model]; dup {
			return fmt.Errorf("model %q is claimed by both rule %q and rule %q", r.Model, prev, r.Name)
		}
		claimed[r.Model] = r.Name

		if err := r.Mints.validate(r.Name); err != nil {
			return err
		}
		if prefixes[r.Mints.Entity] == nil {
			prefixes[r.Mints.Entity] = map[string]string{}
		}
		prefixes[r.Mints.Entity][r.Name] = r.Mints.Prefix

		if err := r.Payload.validate(r.Name, byName); err != nil {
			return err
		}
	}

	for m := range x.Models {
		if _, ok := claimed[m]; !ok {
			return fmt.Errorf("model %q belongs to no rule; every model must belong to exactly one", m)
		}
	}

	// Two rules minting one entity must occupy disjoint key spaces, or the same
	// code can name two different target objects and nothing would notice.
	for entity, byRule := range prefixes {
		names := make([]string, 0, len(byRule))
		for n := range byRule {
			names = append(names, n)
		}
		sort.Strings(names)
		for i, a := range names {
			for _, b := range names[i+1:] {
				pa, pb := byRule[a], byRule[b]
				if strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa) {
					return fmt.Errorf(
						"rules %q and %q both mint %s with overlapping prefixes %q and %q; "+
							"one identity must name exactly one target object, so their key "+
							"spaces have to be disjoint",
						a, b, entity, pa, pb)
				}
			}
		}
	}

	// Every reference must name a rule that mints something.
	for _, r := range x.Rules {
		for field, b := range r.Payload.Fields {
			if b.References == "" {
				continue
			}
			if !byName[b.References] {
				return fmt.Errorf("rule %q: field %q references rule %q, which does not exist",
					r.Name, field, b.References)
			}
		}
	}
	return nil
}

// forbidden are the things a mint must never depend on. A mint that is not a
// pure function of source values makes output depend on insertion order, which
// breaks reproducibility and makes every row of a run diff look changed.
var forbidden = regexp.MustCompile(`(?i)\b(now|random|nextval|uuid|current_(date|time|timestamp)|gen_random)\b`)

func (m Mint) validate(rule string) error {
	if m.Entity == "" {
		return fmt.Errorf("rule %q mints no entity", rule)
	}
	if len(m.From) == 0 {
		return fmt.Errorf("rule %q: mints.from is required — it is the closure a slice follows", rule)
	}
	for _, c := range m.From {
		if forbidden.MatchString(c) {
			return fmt.Errorf("rule %q: mint reads %q, which is not a source value; "+
				"an identity must be a pure function of the source", rule, c)
		}
	}
	if m.Conflict != nil {
		switch m.Conflict.Resolve {
		case "", "error":
		default:
			return fmt.Errorf("rule %q: conflict resolve %q is not supported yet (only \"error\")",
				rule, m.Conflict.Resolve)
		}
	}
	return nil
}

// Rule looks up a rule by name.
func (x *Xform) Rule(name string) (Rule, bool) {
	for _, r := range x.Rules {
		if r.Name == name {
			return r, true
		}
	}
	return Rule{}, false
}

// Targets lists the distinct endpoints this transform writes to, in load order.
func (x *Xform) Targets() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range x.Ordered() {
		if !seen[r.To] {
			seen[r.To] = true
			out = append(out, r.To)
		}
	}
	return out
}

// Ordered returns the rules in the order they must run: by Qniform's load order,
// then by name so two rules writing the same endpoint are stable.
func (x *Xform) Ordered() []Rule {
	out := append([]Rule(nil), x.Rules...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, _ := loadRank(out[i].To)
		rj, _ := loadRank(out[j].To)
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Hash is the Xform's version: the manifest and every model's SQL together.
// Changing one rule changes the transform, which is what makes a Run's input
// triple honest.
func (x *Xform) Hash() string {
	type flat struct {
		Manifest any
		Models   map[string]string
	}
	m := map[string]any{
		"name": x.Name, "version": x.Version,
		"inputs": x.Inputs, "rules": x.Rules,
	}
	return hashJSON(flat{Manifest: m, Models: x.Models})
}

var identRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+"?([a-z_][a-z0-9_]*)"?`)

// ModelDeps reports which input views each model reads, for display. Models no
// longer read one another — every rule reads the datasets directly — so this is
// documentation rather than a dependency order.
func (x *Xform) ModelDeps(inputs map[string]bool) map[string][]string {
	deps := map[string][]string{}
	for name, sql := range x.Models {
		seen := map[string]bool{}
		for _, m := range identRe.FindAllStringSubmatch(sql, -1) {
			if ref := m[1]; inputs[ref] && !seen[ref] {
				seen[ref] = true
				deps[name] = append(deps[name], ref)
			}
		}
		sort.Strings(deps[name])
	}
	return deps
}

// payloadFieldOrder walks the manifest for each rule's payload.fields keys, in
// document order.
func payloadFieldOrder(doc *yaml.Node) [][]string {
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "rules" {
			continue
		}
		var out [][]string
		for _, rule := range root.Content[i+1].Content {
			out = append(out, fieldsOf(rule))
		}
		return out
	}
	return nil
}

func fieldsOf(rule *yaml.Node) []string {
	for i := 0; i+1 < len(rule.Content); i += 2 {
		if rule.Content[i].Value != "payload" {
			continue
		}
		p := rule.Content[i+1]
		for j := 0; j+1 < len(p.Content); j += 2 {
			if p.Content[j].Value != "fields" {
				continue
			}
			f := p.Content[j+1]
			out := make([]string, 0, len(f.Content)/2)
			for k := 0; k+1 < len(f.Content); k += 2 {
				out = append(out, f.Content[k].Value)
			}
			return out
		}
	}
	return nil
}
