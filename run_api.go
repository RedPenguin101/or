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

	// The transform and the target contract come from what this project is
	// pinned to, not from the shipped catalogue. A Run is only reproducible if
	// its inputs are the recorded ones.
	xform, err := a.DB.GetTransform(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	spec, err := a.DB.GetSpec(r.Context(), projectID, TargetSystem)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
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
	in := RunRequest{
		Project: p.Name, Reason: req.Reason,
		Actor:     Actor{Kind: "user", ID: req.Actor},
		Firm:      p.Firm,
		Schemas:   map[string]*SourceSchema{},
		Datasets:  map[string]*Dataset{},
		DatasetID: map[string]string{},
		Transform: xform,
		Spec:      spec,
	}
	for _, inp := range xform.Inputs {
		for _, s := range schemas {
			if s.SchemaName == inp.SchemaName {
				in.Schemas[inp.As] = s
			}
		}
		for i := range datasets { // ListDatasets is newest first
			if datasets[i].SchemaName == inp.SchemaName {
				d := datasets[i].Dataset
				in.Datasets[inp.As] = &d
				in.DatasetID[inp.As] = datasets[i].ID
				in.WithheldAtIngest = append(in.WithheldAtIngest, datasets[i].Rejects...)
				break
			}
		}
		if in.Datasets[inp.As] == nil {
			writeErr(w, http.StatusBadRequest,
				fmt.Errorf("no dataset uploaded for %s, which the transform needs as %q", inp.SchemaName, inp.As))
			return
		}
	}

	in.Number, err = a.DB.NextRunNumber(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	a.runMu.Lock()
	run, err := ExecuteRun(a.Engine, a.Store, in)
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
