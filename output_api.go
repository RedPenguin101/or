package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

func (a *App) registerOutputRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/outputs/{name}", a.getOutput)
}

// outputView pairs what Qniform accepts with what the rules actually send.
//
// The spec alone would be a documentation viewer. The useful question is the
// join: every field the target takes, marked with whether anything binds it —
// which is how "what have we not mapped yet" stops being folklore.
type outputView struct {
	Name   string     `json:"name"`
	Title  string     `json:"title"`
	Target string     `json:"target"`
	Entity string     `json:"entity"`
	After  []string   `json:"after"`
	Batch  Batch      `json:"batch"`
	Doc    *TargetDoc `json:"doc,omitempty"`
	DocErr string     `json:"doc_error,omitempty"`
	Fields []fieldCov `json:"fields"`
	Rules  []Rule     `json:"rules"`
	Extra  []string   `json:"unknown_fields"`
}

// fieldCov is one field of the target's request body, and what we do about it.
type fieldCov struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`

	Bound      bool   `json:"bound"`
	References string `json:"references,omitempty"`
	KeyFrom    string `json:"key_from,omitempty"`
}

func (a *App) getOutput(w http.ResponseWriter, r *http.Request) {
	p, err := a.DB.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	mt, err := a.Type(p.MigrationType)
	if err != nil || mt.Xform == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no xform for migration type %s", p.MigrationType))
		return
	}
	x := mt.Xform

	name := r.PathValue("name")
	var out *Output
	for i := range x.Outputs {
		if x.Outputs[i].Name == name {
			out = &x.Outputs[i]
		}
	}
	if out == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no output %q", name))
		return
	}

	em := x.Emitters[out.Emitter]
	v := outputView{
		Name: out.Name, Title: out.Title, Target: out.Target,
		Entity: out.Entity, After: out.After, Rules: []Rule{},
		Fields: []fieldCov{}, Extra: []string{},
	}
	if em != nil {
		v.Batch = em.Batch
	}
	for _, rule := range x.Rules {
		if rule.To == out.Name {
			v.Rules = append(v.Rules, rule)
		}
	}

	// Without the spec there is no coverage to report, only bindings. Say so
	// rather than presenting a partial answer as a complete one.
	if a.Spec == nil {
		v.DocErr = "Qniform's OpenAPI spec is not loaded, so the target's own shape is unknown"
		writeJSON(w, http.StatusOK, v)
		return
	}
	doc, err := a.Spec.Describe(out.Target)
	if err != nil {
		v.DocErr = err.Error()
		writeJSON(w, http.StatusOK, v)
		return
	}
	v.Doc = doc

	required := map[string]bool{}
	if doc.Item != nil {
		for _, rq := range doc.Item.Required {
			required[rq] = true
		}
	}

	names := []string{}
	if doc.Item != nil {
		for n := range doc.Item.Properties {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	// Required fields first, then bound ones, then the rest: the reader is
	// looking for gaps, and a gap in a required field is the urgent kind.
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := required[names[i]], required[names[j]]
		if ri != rj {
			return ri
		}
		_, bi := em.Fields[names[i]]
		_, bj := em.Fields[names[j]]
		return bi && !bj
	})

	for _, n := range names {
		ps := doc.Item.Properties[n]
		fc := fieldCov{Name: n, Required: required[n]}
		if ps != nil {
			fc.Type, fc.Enum, fc.Description = ps.Type, ps.Enum, ps.Description
			switch {
			case ps.Type == "array" && ps.Items != nil:
				fc.Type = "array of " + ps.Items.Type
			case ps.Ref != "":
				// A $ref carries no inline type. Name the component instead of
				// showing a blank cell that reads as "unknown".
				fc.Type = ps.Ref[strings.LastIndex(ps.Ref, "/")+1:]
			}
		}
		if b, ok := em.Fields[n]; ok {
			// The model column carries this field's own name, so there is no
			// separate source to report — which is the point.
			fc.Bound, fc.References = true, b.References
			if b.Map != nil {
				fc.KeyFrom = b.Map.KeyFrom
			}
		}
		v.Fields = append(v.Fields, fc)
	}

	// A bound field the spec does not have would be rejected by Qniform. The
	// batch validator already catches it at run time; showing it here catches it
	// while the rule is being read.
	if doc.Item != nil {
		for n := range em.Fields {
			if _, ok := doc.Item.Properties[n]; !ok {
				v.Extra = append(v.Extra, n)
			}
		}
		sort.Strings(v.Extra)
	}

	writeJSON(w, http.StatusOK, v)
}
