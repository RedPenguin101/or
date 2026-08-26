package main

import (
	"fmt"
	"net/http"
)

func (a *App) registerSchemaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/schemas", a.listSchemas)
	mux.HandleFunc("GET /api/projects/{id}/xform", a.getXform)
}

// schemaView carries the hash alongside the schema. The hash is the version, and
// it is what a Run pins to, so it belongs in every view of a schema rather than
// being something the UI has to ask for separately.
type schemaView struct {
	*SourceSchema
	Hash string `json:"hash"`
}

func (a *App) listSchemas(w http.ResponseWriter, r *http.Request) {
	ss, err := a.DB.ListSchemas(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]schemaView, 0, len(ss))
	for _, s := range ss {
		out = append(out, schemaView{SourceSchema: s, Hash: s.Hash()})
	}
	writeJSON(w, http.StatusOK, out)
}

// xformView exposes the rules as the artifacts they are — the manifest, the
// model SQL, the emitters — rather than as an opaque "transformation". Iteration
// 1 does not let the user edit them, but it must let the user *read* them.
//
// The shape is ordered for how it is read: Rules bind Inputs to Outputs, and
// that binding is the summary. Models, entities and emitters are the detail
// behind one rule, and shared models are implementation with no place in the
// summary at all.
type xformView struct {
	Name         string              `json:"name"`
	Version      int                 `json:"version"`
	Description  string              `json:"description"`
	Hash         string              `json:"hash"`
	Inputs       []Input             `json:"inputs"`
	Outputs      []Output            `json:"outputs"`
	Rules        []ruleView          `json:"rules"`
	Entities     []Entity            `json:"entities"`
	Models       map[string]string   `json:"models"`
	SharedModels []string            `json:"shared_models"`
	ModelDeps    map[string][]string `json:"model_deps"`
	Emitters     map[string]*Emitter `json:"emitters"`
}

// ruleView is a Rule with the detail a reader needs resolved onto it, so the
// client does not have to join four collections to render one card.
type ruleView struct {
	Rule
	OutputTitle string    `json:"output_title"`
	Target      string    `json:"target"`
	Entity      string    `json:"entity"`
	Identity    *Identity `json:"identity,omitempty"`
	Emitter     string    `json:"emitter"`
}

func (a *App) getXform(w http.ResponseWriter, r *http.Request) {
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
	inputs := map[string]bool{}
	for _, in := range x.Inputs {
		inputs[in.As] = true
	}
	_, deps, err := x.ModelOrder(inputs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	rules := make([]ruleView, 0, len(x.Rules))
	for _, r := range x.Rules {
		rv := ruleView{Rule: r}
		for _, o := range x.Outputs {
			if o.Name != r.To {
				continue
			}
			rv.OutputTitle, rv.Target, rv.Entity, rv.Emitter = o.Title, o.Target, o.Entity, o.Emitter
			if e, ok := x.Entity(o.Entity); ok {
				id := e.Identity
				rv.Identity = &id
			}
		}
		rules = append(rules, rv)
	}

	writeJSON(w, http.StatusOK, xformView{
		Name: x.Name, Version: x.Version, Description: x.Description, Hash: x.Hash(),
		Inputs: x.Inputs, Outputs: x.Outputs, Rules: rules, Entities: x.Entities,
		Models: x.Models, ModelDeps: deps,
		Emitters: x.Emitters,
	})
}
