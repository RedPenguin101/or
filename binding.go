package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// TargetBinding is the last leg of a rule: binding the flat rows its SQL produced
// to the nested JSON the target accepts.
//
// It lives on the rule rather than on the endpoint so a rule carries its whole
// chain — source, SQL, binding — and nothing downstream reshapes what it makes.
// Two rules writing the same endpoint each own their own, which is why a rule
// that always produces one role can state that role as a literal instead of
// carrying a column to vary it.
//
// There is deliberately no `from` and no conditional: a model column carries the
// name of the field it becomes, and absence is a NULL in the model, which is
// omitted by a fixed rule nobody can configure. What is left has no expression
// syntax, so it cannot express a business decision.
type TargetBinding struct {
	RowKey string          `yaml:"row_key" json:"row_key"`
	Fields map[string]Bind `yaml:"fields"  json:"fields"`
	Order  []string        `yaml:"-"       json:"order"` // authored order, for display
}

// Bind is one target field.
type Bind struct {
	// References names the rule whose identity this field carries. The value in
	// the model is the raw source key; the minting rule's prefix is applied here,
	// so a reference never restates the mint expression and the two cannot drift.
	References string `yaml:"references" json:"references,omitempty"`

	// KeyFrom builds a single-key object whose key comes from a column — the
	// shape Qniform's PartyRoles uses, where a role is held by virtue of its key
	// being present. The one thing SQL cannot produce.
	KeyFrom string `yaml:"key_from" json:"key_from,omitempty"`

	// Object is a literal: a constant shape with no column behind it. A literal
	// cannot compute anything, so it adds no expressiveness — only the ability to
	// say "this rule always produces exactly this".
	Object map[string]any `yaml:"object" json:"object,omitempty"`
}

func (p *TargetBinding) normalise() {
	if p.RowKey == "" {
		p.RowKey = "code"
	}
}

// SetOrder records the authored field order from the raw YAML node, so an
// emitted payload reads the way its author wrote it.
func (p *TargetBinding) SetOrder(order []string) { p.Order = order }

func (p *TargetBinding) validate(rule string, _ map[string]bool) error {
	if len(p.Fields) == 0 {
		return fmt.Errorf("rule %q has no payload fields", rule)
	}
	for name, b := range p.Fields {
		n := 0
		if b.KeyFrom != "" {
			n++
		}
		if b.Object != nil {
			n++
		}
		if n > 1 {
			return fmt.Errorf("rule %q: field %q sets both key_from and object", rule, name)
		}
	}
	if _, ok := p.Fields[p.RowKey]; !ok {
		return fmt.Errorf("rule %q: row_key %q is not one of the payload fields", rule, p.RowKey)
	}
	return nil
}

func (p *TargetBinding) fieldNames() []string {
	if len(p.Order) == len(p.Fields) {
		return p.Order
	}
	out := make([]string, 0, len(p.Fields))
	for k := range p.Fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Columns is every model column this payload reads: the fields it sends and any
// column a nesting key comes from. A column outside this set does no work, which
// is how a vocabulary starts to grow between source and target.
func (p *TargetBinding) Columns() map[string]bool {
	used := map[string]bool{p.RowKey: true}
	for _, name := range p.fieldNames() {
		b := p.Fields[name]
		switch {
		case b.KeyFrom != "":
			used[b.KeyFrom] = true
		case b.Object != nil: // a literal reads nothing
		default:
			used[name] = true
		}
	}
	return used
}

// Compile builds the SELECT that feeds this payload: the columns it reads, in a
// fixed order. A plain projection — there is nothing left to evaluate.
func (p *TargetBinding) Compile(table string) string {
	var sel []string
	seen := map[string]bool{}
	add := func(col string) {
		if col != "" && !seen[col] {
			seen[col] = true
			sel = append(sel, quoteIdent(col))
		}
	}
	add(p.RowKey)
	for _, name := range p.fieldNames() {
		b := p.Fields[name]
		switch {
		case b.KeyFrom != "":
			add(b.KeyFrom)
		case b.Object != nil:
		default:
			add(name)
		}
	}
	add("__row")
	// Ordered so a model emitting several rows per identity always presents the
	// same one first; without it the payload would depend on scan order, which
	// breaks reproducibility and run diffing alike.
	return fmt.Sprintf("SELECT %s FROM %s ORDER BY %s, %s",
		joinComma(sel), quoteIdent(table), quoteIdent(p.RowKey), quoteIdent("__row"))
}

// Build turns one compiled row into the request body item. mintRef applies a
// referenced rule's prefix to a raw source key.
func (p *TargetBinding) Build(row map[string]any, mintRef func(rule string, raw string) string) *OrderedMap {
	out := NewOrderedMap()
	for _, name := range p.fieldNames() {
		b := p.Fields[name]

		if b.Object != nil {
			out.Set(name, b.Object)
			continue
		}
		if b.KeyFrom != "" {
			key, _ := row[b.KeyFrom].(string)
			if key == "" {
				continue
			}
			inner := NewOrderedMap()
			inner.Set(key, map[string]any{})
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
		if b.References != "" {
			raw, _ := v.(string)
			if raw == "" {
				continue
			}
			v = mintRef(b.References, raw)
		}
		out.Set(name, v)
	}
	return out
}

// Template is the payload this binding emits, with a placeholder standing in
// for every value a row would supply.
//
// It runs the emitter — Build, over a synthetic row — rather than describing
// what the emitter would do, so what a rule's page shows and what a Run sends
// are one implementation. A second one, written to render the binding for a
// reader, is a place for the two to disagree, and a user reading the page would
// be the last to find out. Every column is present, so nothing is omitted and
// the shape is whole; a NULL is the one thing a template cannot show, which is
// why the page says so in prose instead.
func (p *TargetBinding) Template(placeholder func(column string) string, mintRef func(rule, raw string) string) *OrderedMap {
	row := map[string]any{}
	for col := range p.Columns() {
		row[col] = placeholder(col)
	}
	return p.Build(row, mintRef)
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// OrderedMap keeps emitted payloads in the order the binding declares them.
// Field order carries no meaning to Qniform but a great deal to a human reading
// a diff, and the whole product is about making that reading easy.
type OrderedMap struct {
	keys []string
	vals map[string]any
}

func NewOrderedMap() *OrderedMap { return &OrderedMap{vals: map[string]any{}} }

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
