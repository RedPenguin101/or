package main

import (
	"fmt"
	"net/http"
	"strings"
)

func (a *App) registerRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/runs", a.listRuns)
	mux.HandleFunc("POST /api/projects/{id}/runs", a.createRun)
	mux.HandleFunc("GET /api/runs/{rid}", a.getRun)
}

func (a *App) listRuns(w http.ResponseWriter, r *http.Request) {
	rs, err := a.DB.ListRuns(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (a *App) createRun(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	p, err := a.DB.GetProject(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
		Tier   string `json:"tier"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Refused rather than defaulted. A Run whose reason is blank is a Run nobody
	// can explain in six months, which is the failure the audit trail exists to
	// prevent.
	if strings.TrimSpace(req.Reason) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("a reason is required for every run"))
		return
	}
	if req.Actor == "" {
		req.Actor = "unknown"
	}
	if req.Tier == "" {
		req.Tier = "full"
	}

	mt, err := a.Type(p.MigrationType)
	if err != nil || mt.Xform == nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no xform for migration type %s", p.MigrationType))
		return
	}

	datasets, err := a.DB.ListDatasets(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	schemas, err := a.DB.ListSchemas(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Bind each declared input to the most recent dataset for its schema.
	in := ApplyInput{
		Project: p.Name, Tier: req.Tier, Reason: req.Reason,
		Actor:    Actor{Kind: "user", ID: req.Actor},
		Firm:     p.Firm,
		Schemas:  map[string]*SourceSchema{},
		Datasets: map[string]*Dataset{},
		Xform:    mt.Xform,
		Spec:     a.Spec,
	}
	for _, inp := range mt.Xform.Inputs {
		for _, s := range schemas {
			if s.Ref() == inp.Schema {
				in.Schemas[inp.As] = s
			}
		}
		for i := range datasets { // ListDatasets is newest first
			if datasets[i].SchemaRef == inp.Schema {
				d := datasets[i].Dataset
				in.Datasets[inp.As] = &d
				in.Prior = append(in.Prior, datasets[i].Rejects...)
				break
			}
		}
		if in.Datasets[inp.As] == nil {
			writeErr(w, http.StatusBadRequest,
				fmt.Errorf("no dataset uploaded for %s, which the xform needs as %q", inp.Schema, inp.As))
			return
		}
	}

	number, err := a.DB.NextRunNumber(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	a.runMu.Lock()
	run, err := Apply(a.Engine, in, number)
	a.runMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	id := newID("run")
	if err := a.DB.PutRun(r.Context(), id, projectID, run); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "run": run})
}

func (a *App) getRun(w http.ResponseWriter, r *http.Request) {
	body, err := a.DB.GetRun(r.Context(), r.PathValue("rid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}
