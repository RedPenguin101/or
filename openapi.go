package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec describes the shape of the target's data: what Qniform will accept at
// each endpoint.
//
// It is a description of a data format, in the same category as a SourceSchema —
// one describes the source's shape, the other the target's. It happens to be
// written as OpenAPI because that is a convenient way to describe an API and
// because it is what Qniform emits, not because OnRamp needs OpenAPI.
//
// The copy under targets/ is OnRamp's own, checked into this repository.
// Reading it live from the Qniform repository made a foreign, unpinnable file
// part of the deterministic contract, and left a Run recording a hash nothing
// could resolve back to content. Keeping drift in step with real Qniform is a
// separate, out-of-band concern — see spec-check.sh.
type Spec struct {
	Paths map[string]map[string]operation `yaml:"paths"`
	path  string

	// raw is the document exactly as authored. Kept because it, not the parsed
	// struct, is what gets stored and hashed: OnRamp parses a fraction of
	// OpenAPI, and a body reduced to the fraction we understand would lose the
	// rest on the first round trip.
	raw []byte
}

type operation struct {
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
	RequestBody struct {
		Content map[string]struct {
			Schema *jsonSchema `yaml:"schema"`
		} `yaml:"content"`
	} `yaml:"requestBody"`
}

type jsonSchema struct {
	Type        string                 `yaml:"type"        json:"type,omitempty"`
	Description string                 `yaml:"description" json:"description,omitempty"`
	Format      string                 `yaml:"format"      json:"format,omitempty"`
	Default     any                    `yaml:"default"     json:"default,omitempty"`
	Example     any                    `yaml:"example"     json:"example,omitempty"`
	Required    []string               `yaml:"required"    json:"required,omitempty"`
	Properties  map[string]*jsonSchema `yaml:"properties"  json:"properties,omitempty"`
	Items       *jsonSchema            `yaml:"items"       json:"items,omitempty"`
	Enum        []string               `yaml:"enum"        json:"enum,omitempty"`
	MaxItems    int                    `yaml:"maxItems"    json:"maxItems,omitempty"`
	Ref         string                 `yaml:"$ref"        json:"$ref,omitempty"`
}

// LoadSpec reads the seed document from disk — the copy under targets/, which
// a human adopts changes into. Runs read the project's stored spec instead.
func LoadSpec(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := ParseSpec(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.path = path
	return s, nil
}

// ParseSpec reads a spec from its document body, which is the form the store
// keeps and a Run consumes.
func ParseSpec(raw []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	s.raw = raw
	return &s, nil
}

// Body is the document as authored, byte for byte.
func (s *Spec) Body() []byte { return s.raw }

// Hash identifies the whole document, which is what the store is keyed by.
//
// Distinct from HashFor, and deliberately: HashFor is the version of one
// endpoint's contract, which is what a Run records, because only the endpoints a
// transform writes are inputs to it. This one answers a different question —
// which document is this — and a Run needs both: the per-endpoint hashes to say
// what it depended on, this one to find the bytes again.
func (s *Spec) Hash() string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(s.raw))[:19]
}

// TargetDoc is what the target system says about one endpoint: its own prose and
// the shape it accepts. Read alongside what the rules actually bind (see
// output_api.go), it answers the question a migration runs on — what will
// Qniform take, and how much of it are we sending?
type TargetDoc struct {
	Target      string      `json:"target"`
	Summary     string      `json:"summary"`
	Description string      `json:"description"`
	MaxItems    int         `json:"max_items"`
	Item        *jsonSchema `json:"item"`
}

// Describe returns the target's own documentation for an emitter target.
func (s *Spec) Describe(target string) (*TargetDoc, error) {
	parts := strings.Fields(target)
	if len(parts) != 2 {
		return nil, fmt.Errorf("target %q is not \"METHOD /path\"", target)
	}
	op, ok := s.Paths[parts[1]][strings.ToLower(parts[0])]
	if !ok {
		return nil, fmt.Errorf("%s: %s is not in the spec", s.path, target)
	}
	root, err := s.Operation(target)
	if err != nil {
		return nil, err
	}
	doc := &TargetDoc{
		Target:      target,
		Summary:     op.Summary,
		Description: strings.TrimSpace(op.Description),
		Item:        root,
	}
	if root.Type == "array" && root.Items != nil {
		doc.MaxItems, doc.Item = root.MaxItems, root.Items
	}
	return doc, nil
}

// Operation resolves an emitter target like "PUT /firms/{firm}/accounts".
func (s *Spec) Operation(target string) (*jsonSchema, error) {
	parts := strings.Fields(target)
	if len(parts) != 2 {
		return nil, fmt.Errorf("target %q is not \"METHOD /path\"", target)
	}
	method, path := strings.ToLower(parts[0]), parts[1]
	ops, ok := s.Paths[path]
	if !ok {
		return nil, fmt.Errorf("%s: path %s is not in the spec", s.path, path)
	}
	op, ok := ops[method]
	if !ok {
		return nil, fmt.Errorf("%s: %s has no %s operation", s.path, path, strings.ToUpper(method))
	}
	for _, c := range op.RequestBody.Content {
		if c.Schema != nil {
			return c.Schema, nil
		}
	}
	return nil, fmt.Errorf("%s %s has no request body schema", parts[0], path)
}

// ValidateBatch checks one rule's payloads against the operation's request body.
//
// It covers what the spec states mechanically — item cap, required fields,
// enums, unknown properties. Conditional rules stated only in prose (that
// counterparty is *rejected* on an inventory account, say) are not machine
// readable and remain the emitter author's responsibility; that gap is the
// argument for eventually generating emitters from the spec rather than
// checking them against it.
func (s *Spec) ValidateBatch(target string, payloads []*OrderedMap) ([]Reject, error) {
	root, err := s.Operation(target)
	if err != nil {
		return nil, err
	}
	item := root
	if root.Type == "array" {
		if root.Items == nil {
			return nil, fmt.Errorf("%s: array request body with no item schema", target)
		}
		item = root.Items
	}
	if item.Ref != "" {
		// Iteration 1's two targets both inline their item schema. A $ref would
		// need a component resolver, and failing loudly is better than silently
		// validating nothing.
		return nil, fmt.Errorf("%s: item schema is a $ref (%s), which is not resolved yet", target, item.Ref)
	}

	var out []Reject
	for _, p := range payloads {
		code, _ := p.Get("code")
		id, _ := code.(string)

		for _, req := range item.Required {
			if _, ok := p.Get(req); !ok {
				out = append(out, Reject{
					Class: RejectRow, Phase: PhaseValidation, Entity: target,
					Identity: id, Field: req,
					Message: fmt.Sprintf("required by the spec but not emitted: %s", req),
				})
			}
		}
		for _, k := range p.keys {
			ps, ok := item.Properties[k]
			if !ok {
				out = append(out, Reject{
					Class: RejectRow, Phase: PhaseValidation, Entity: target,
					Identity: id, Field: k,
					Message: fmt.Sprintf("%q is not a property of this request body", k),
				})
				continue
			}
			if len(ps.Enum) == 0 {
				continue
			}
			v, _ := p.Get(k)
			sv, isStr := v.(string)
			if !isStr {
				continue
			}
			if !containsStr(ps.Enum, sv) {
				sort.Strings(ps.Enum)
				out = append(out, Reject{
					Class: RejectRow, Phase: PhaseValidation, Entity: target,
					Identity: id, Field: k, Value: sv,
					Message: fmt.Sprintf("%q is not one of the spec's values for %s (%s)",
						sv, k, strings.Join(ps.Enum, ", ")),
				})
			}
		}
	}
	if root.MaxItems > 0 && len(payloads) > root.MaxItems {
		out = append(out, Reject{
			Class: RejectRow, Phase: PhaseValidation, Entity: target,
			Message: fmt.Sprintf("%d items exceeds the spec's cap of %d per request",
				len(payloads), root.MaxItems),
		})
	}
	return out, nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// TargetSpecPath is OnRamp's own copy of the target's contract. Checked in, so a
// Run's recorded spec version can always be resolved back to the document that
// produced it.
const TargetSpecPath = "targets/" + TargetSystem + "/openapi.yaml"

// targetSpecPath allows an override for tests and for the drift check, but the
// default is always the vendored copy: a run must never depend on a file this
// repository does not contain.
func targetSpecPath() string {
	if p := os.Getenv("ONRAMP_TARGET_SPEC"); p != "" {
		return p
	}
	return TargetSpecPath
}

// HashFor is the version of one target's contract: the shape Qniform will accept
// at this endpoint.
//
// Hashed per target rather than over the whole document on purpose. The spec is
// an *input* to a Run — validation withholds payloads, so a changed contract
// changes the output — but only the endpoints a transform actually writes are
// inputs to it. A typo in an unrelated endpoint's prose should not invalidate a
// run that never touched it.
func (s *Spec) HashFor(target string) (string, error) {
	doc, err := s.Describe(target)
	if err != nil {
		return "", err
	}
	// Deliberately excludes Summary and Description: prose is documentation, not
	// contract, and a reworded paragraph does not change what Qniform accepts.
	return hashJSON(struct {
		Target   string
		MaxItems int
		Item     *jsonSchema
	}{doc.Target, doc.MaxItems, doc.Item}), nil
}
