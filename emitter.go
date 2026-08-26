package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// An Emitter binds a model's columns to a Qniform request body. It is
// deliberately not a programming language (design.md §3, Layer 3): everything it
// can express is checkable against openapi.yaml before a row is transformed.
//
// It is *compiled* to SQL rather than interpreted, so omit_when and the rest are
// evaluated by the one engine that already exists rather than by a second
// expression evaluator with its own semantics.
type Emitter struct {
	Target string          `yaml:"target"  json:"target"`
	Batch  Batch           `yaml:"batch"   json:"batch"`
	RowKey string          `yaml:"row_key" json:"row_key"`
	Fields map[string]Bind `yaml:"fields"  json:"fields"`
	Order  []string        `yaml:"-"       json:"order"` // authored field order, for display
}

type Batch struct {
	MaxItems    int    `yaml:"max_items"    json:"max_items"`
	FailureMode string `yaml:"failure_mode" json:"failure_mode"` // per_item | all_or_nothing
}

// Bind is one target field.
//
// There is deliberately no `from` and no `omit_when`. A model column carries the
// name of the field it becomes, so renaming here would be a second place to make
// a semantic decision; and absence is expressed in the model as NULL, which the
// emitter turns into an omitted key by a fixed rule nobody can configure.
//
// What is left cannot express a business decision, because there is no
// expression syntax left to express one in.
type Bind struct {
	// References names the entity whose identity this field carries. A
	// declaration, not a transformation: read forwards it orders the outputs,
	// backwards it drives the quarantine cascade.
	References string `yaml:"references" json:"references,omitempty"`

	// Map builds an object whose key comes from a column — the shape Qniform's
	// PartyRoles uses, where a role is held by virtue of its key being present.
	// This is the one thing SQL genuinely cannot produce.
	Map *MapBind `yaml:"map" json:"map,omitempty"`
}

type MapBind struct {
	KeyFrom string         `yaml:"key_from" json:"key_from"`
	Value   map[string]any `yaml:"value"    json:"value"`
}

func LoadEmitter(path string) (*Emitter, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Emitter
	if err := yaml.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// yaml.v3 loses map order, so recover the authored order from the raw node:
	// the emitted payload should read the way the author wrote it.
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err == nil {
		e.Order = fieldOrder(&doc)
	}
	if e.Target == "" {
		return nil, fmt.Errorf("%s: emitter has no target", path)
	}
	if e.RowKey == "" {
		return nil, fmt.Errorf("%s: emitter has no row_key", path)
	}
	if e.Batch.MaxItems == 0 {
		e.Batch.MaxItems = 500
	}
	return &e, nil
}

func fieldOrder(doc *yaml.Node) []string {
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "fields" {
			continue
		}
		m := root.Content[i+1]
		out := make([]string, 0, len(m.Content)/2)
		for j := 0; j+1 < len(m.Content); j += 2 {
			out = append(out, m.Content[j].Value)
		}
		return out
	}
	return nil
}

func (e *Emitter) fieldNames() []string {
	if len(e.Order) == len(e.Fields) {
		return e.Order
	}
	out := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Compile builds the SELECT that feeds this emitter: the columns it sends, plus
// any column a nesting key is read from. A plain projection — there is nothing
// left to evaluate.
func (e *Emitter) Compile(model string) string {
	var sel []string
	seen := map[string]bool{}
	add := func(col string) {
		if col != "" && !seen[col] {
			seen[col] = true
			sel = append(sel, quoteIdent(col))
		}
	}
	add(e.RowKey)
	for _, name := range e.fieldNames() {
		b := e.Fields[name]
		if b.Map != nil {
			add(b.Map.KeyFrom)
			continue
		}
		add(name) // the column carries the target field's own name
	}
	add("__row")
	// Ordered so that a model emitting several rows per identity always presents
	// the same one first. Without this the payload would depend on scan order,
	// which would quietly break both reproducibility and run diffing.
	return fmt.Sprintf("SELECT %s FROM %s ORDER BY %s, %s",
		strings.Join(sel, ", "), quoteIdent(model), quoteIdent(e.RowKey), quoteIdent("__row"))
}

// Payload turns one compiled row into the request body item.
func (e *Emitter) Payload(row map[string]any) *OrderedMap {
	out := NewOrderedMap()
	for _, name := range e.fieldNames() {
		b := e.Fields[name]

		if b.Map != nil {
			key, _ := row[b.Map.KeyFrom].(string)
			if key == "" {
				continue
			}
			inner := NewOrderedMap()
			v := b.Map.Value
			if v == nil {
				v = map[string]any{}
			}
			inner.Set(key, v)
			out.Set(name, inner)
			continue
		}
		// NULL means the key is left out entirely, which is not the same as
		// sending null. This is the whole mechanism for absence, and it is fixed:
		// the model decides, by producing NULL or not.
		v := row[name]
		if v == nil {
			continue
		}
		out.Set(name, v)
	}
	return out
}

// OrderedMap keeps emitted payloads in the order the emitter declares them.
// Field order carries no meaning to Qniform but a great deal to a human reading
// a diff, and the whole product is about making that reading easy.
type OrderedMap struct {
	keys []string
	vals map[string]any
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{vals: map[string]any{}}
}

func (m *OrderedMap) Set(k string, v any) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *OrderedMap) Get(k string) (any, bool) { v, ok := m.vals[k]; return v, ok }
func (m *OrderedMap) Len() int                 { return len(m.keys) }

func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UsedColumns is every model column this emitter reads: the columns it sends,
// the columns nesting keys come from, and the row key.
//
// A column outside this set does no work — not sent, not read, not referenced —
// and an accumulation of them is how a vocabulary grows between source and
// target. Naming the set is what makes that detectable rather than invisible.
func (e *Emitter) UsedColumns() map[string]bool {
	used := map[string]bool{e.RowKey: true}
	for _, name := range e.fieldNames() {
		if b := e.Fields[name]; b.Map != nil {
			used[b.Map.KeyFrom] = true
			continue
		}
		used[name] = true
	}
	return used
}
