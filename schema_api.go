package main

import (
	"net/http"
)

func (a *App) registerSchemaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/schemas", a.listSchemas)
	mux.HandleFunc("GET /api/projects/{id}/transform", a.getTransform)
}

// schemaView carries the hash alongside the schema. The hash is the version, and
// it is what a Run pins to, so it belongs in every view of a schema rather than
// being something the UI has to ask for separately.
type schemaView struct {
	*SourceSchema
	Hash Hash `json:"hash"`
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

// transformView exposes the rules as the artifacts they are — the manifest and the
// model SQL — rather than as an opaque "transformation". Iteration 1 does not
// let the user edit them, but it must let the user *read* them.
//
// A Rule now carries its whole chain, so nothing has to be joined onto it: the
// target, the mint and the payload shape are all on the rule itself.
type transformView struct {
	Name        string              `json:"name"`
	Version     int                 `json:"version"`
	Description string              `json:"description"`
	Hash        Hash                `json:"hash"`
	Inputs      []Input             `json:"inputs"`
	Targets     []string            `json:"targets"`
	Rules       []Rule              `json:"rules"`
	SQL         map[string]string   `json:"sql"`
	SQLReads    map[string][]string `json:"sql_reads"`
}

func (a *App) getTransform(w http.ResponseWriter, r *http.Request) {
	m, err := a.DB.GetMapping(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	x := m.Transform
	inputs := map[string]bool{}
	for _, in := range x.Inputs {
		inputs[in.As] = true
	}
	writeJSON(w, http.StatusOK, transformView{
		Name: x.Name, Version: x.Version, Description: x.Description, Hash: x.Hash(),
		Inputs: x.Inputs, Targets: x.Targets(), Rules: x.Ordered(),
		SQL: x.SQL, SQLReads: x.SQLReads(inputs),
	})
}
