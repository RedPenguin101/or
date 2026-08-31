package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SchemaName identifies a Source Schema: the Source System it came from, and its
// name within that system.
//
// A pair, not a joined "FAS/ACCOUNT-MASTER" string. Everything that handles one
// needs both halves, so joining them only to split them again buys nothing and
// costs a parser — one that meets its first name containing a slash sooner or
// later. Go compares structs with ==, so testing identity needs no normalising.
type SchemaName struct {
	SourceSystem string `yaml:"source_system" json:"source_system"`
	Schema       string `yaml:"schema"        json:"schema"`
}

// String is for display and error messages. Nothing parses it back.
func (n SchemaName) String() string { return n.SourceSystem + "/" + n.Schema }

// A SourceSchema is a named "table" from a Source System: the per-field names,
// types, descriptions and required flags a Dataset is validated against.
//
// It is content-addressed. Its hash is one of the four input versions that make
// a Run a pure function (devnotes/design.md §4), so the canonical form below is
// load-bearing, not a convenience.
type SourceSchema struct {
	SchemaName  `yaml:",inline"`
	Title       string  `yaml:"title"         json:"title"`
	Description string  `yaml:"description"   json:"description"`
	Layout      string  `yaml:"layout"        json:"layout"` // csv | fixed (fixed reserved)
	Header      bool    `yaml:"header"        json:"header"`
	Fields      []Field `yaml:"fields"        json:"fields"`
}

type Field struct {
	Name        string            `yaml:"name"        json:"name"`
	Type        LogicalType       `yaml:"type"        json:"type"`
	Required    bool              `yaml:"required"    json:"required"`
	Description string            `yaml:"description" json:"description"`
	Encoding    Encoding          `yaml:"encoding"    json:"encoding"`
	Scale       int               `yaml:"scale"       json:"scale,omitempty"`
	Values      map[string]string `yaml:"values"      json:"values,omitempty"`
	NullValues  []string          `yaml:"null_values" json:"null_values,omitempty"`
}

// Field looks up a field by its source name.
func (s *SourceSchema) Field(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Hash is the schema's version. Descriptions are included deliberately: a
// changed description is a changed understanding of the field, which is exactly
// the kind of knowledge the audit trail exists to preserve.
func (s *SourceSchema) Hash() string { return hashJSON(s) }

// hashJSON canonicalises through encoding/json, which sorts map keys, so the
// hash is stable across runs and across YAML formatting changes.
func hashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("canonicalise: %v", err)) // struct types only; cannot fail
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))[:19]
}

func LoadSourceSchema(path string) (*SourceSchema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s SourceSchema
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

func (s *SourceSchema) Validate() error {
	if s.SourceSystem == "" || s.Schema == "" {
		return fmt.Errorf("source_system and schema are required")
	}
	if s.Layout != "csv" {
		return fmt.Errorf("layout %q: only csv is supported in iteration 1", s.Layout)
	}
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema has no fields")
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if seen[f.Name] {
			return fmt.Errorf("duplicate field %q", f.Name)
		}
		seen[f.Name] = true
		switch f.Type {
		case TypeString, TypeInteger, TypeDecimal, TypeDate, TypeBoolean, TypeEnum:
		default:
			return fmt.Errorf("field %q: unknown type %q", f.Name, f.Type)
		}
		switch f.Encoding.Kind {
		case "", EncTrim, EncCYYMMDD, EncOverpunchTrailing, EncYN, EncPackedDecimal:
		default:
			return fmt.Errorf("field %q: unknown encoding %q", f.Name, f.Encoding.Kind)
		}
		if f.Type == TypeEnum && len(f.Values) == 0 {
			return fmt.Errorf("field %q: enum with no declared values", f.Name)
		}
	}
	return nil
}

// FieldNames returns source field names in declaration order.
func (s *SourceSchema) FieldNames() []string {
	out := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		out[i] = f.Name
	}
	return out
}
