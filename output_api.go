package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

func (a *App) registerOutputRoutes(mux *http.ServeMux) {
	// A target is "PUT /firms/{firm}/accounts" — spaces, slashes and braces. It
	// travels as a query parameter rather than a path segment so nothing has to
	// be escaped into a shape the router will argue with.
	mux.HandleFunc("GET /api/projects/{id}/target", a.getTarget)
}

// targetView pairs what Qniform accepts with what the rules actually send.
//
// The spec alone would be a documentation viewer. The useful question is the
// join: every field the target takes, marked with which rules populate it —
// which is how "what have we not mapped yet" stops being folklore.
//
// Several rules can write one endpoint, so coverage is a union across them.
type targetView struct {
	Target   string     `json:"target"`
	LoadRank int        `json:"load_rank"`
	Doc      *TargetDoc `json:"doc,omitempty"`
	DocErr   string     `json:"doc_error,omitempty"`
	Fields   []fieldCov `json:"fields"`
	Rules    []Rule     `json:"rules"`
	Extra    []string   `json:"unknown_fields"`
}

// fieldCov is one field of the target's request body, and what we do about it.
type fieldCov struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`

	// Bound names the rules that populate this field — a union, because several
	// rules can feed one endpoint and each may cover a different part of it.
	Bound      []string `json:"bound"`
	References string   `json:"references,omitempty"`
	KeyFrom    string   `json:"key_from,omitempty"`
	Literal    bool     `json:"literal,omitempty"`
}

func (a *App) getTarget(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	mapping, err := a.DB.GetMapping(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	target := r.URL.Query().Get("t")
	if target == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("t (the target endpoint) is required"))
		return
	}

	var rules []Rule
	for _, rule := range mapping.Transform.Ordered() {
		if rule.To == target {
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no rule writes to %q", target))
		return
	}

	rank, _ := loadRank(target)
	v := targetView{Target: target, LoadRank: rank, Rules: rules, Fields: []fieldCov{}, Extra: []string{}}

	// Coverage is reported against the contract this project is pinned to, which
	// is the one its Runs validate against. Without it there is no coverage to
	// report, only bindings — say so rather than presenting a partial answer as
	// a complete one.
	doc, err := mapping.Spec.Describe(target)
	if err != nil {
		v.DocErr = err.Error()
		writeJSON(w, http.StatusOK, v)
		return
	}
	v.Doc = doc

	required := map[string]bool{}
	names := []string{}
	if doc.Item != nil {
		for _, rq := range doc.Item.Required {
			required[rq] = true
		}
		for n := range doc.Item.Properties {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	bound := map[string][]string{}
	detail := map[string]Bind{}
	for _, rule := range rules {
		for name, b := range rule.Binding.Fields {
			bound[name] = append(bound[name], rule.Name)
			if _, seen := detail[name]; !seen {
				detail[name] = b
			}
		}
	}
	for n := range bound {
		sort.Strings(bound[n])
	}

	// Required fields first, then bound ones, then the rest: the reader is
	// looking for gaps, and a gap in a required field is the urgent kind.
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := required[names[i]], required[names[j]]
		if ri != rj {
			return ri
		}
		return len(bound[names[i]]) > 0 && len(bound[names[j]]) == 0
	})

	for _, n := range names {
		ps := doc.Item.Properties[n]
		fc := fieldCov{Name: n, Required: required[n], Bound: bound[n]}
		if fc.Bound == nil {
			fc.Bound = []string{}
		}
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
		if b, ok := detail[n]; ok {
			fc.References, fc.KeyFrom = b.References, b.KeyFrom
			fc.Literal = b.Object != nil
		}
		v.Fields = append(v.Fields, fc)
	}

	// A bound field the spec does not have would be rejected by Qniform. The
	// batch validator catches it at run time; showing it here catches it while
	// the rule is being read.
	if doc.Item != nil {
		for n := range bound {
			if _, ok := doc.Item.Properties[n]; !ok {
				v.Extra = append(v.Extra, n)
			}
		}
		sort.Strings(v.Extra)
	}

	writeJSON(w, http.StatusOK, v)
}
