package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
)

func (a *App) registerDatasetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/datasets", a.listDatasets)
	mux.HandleFunc("POST /api/projects/{id}/datasets", a.uploadDataset)
	mux.HandleFunc("GET /api/projects/{id}/datasets/{did}/rows", a.datasetRows)
}

func (a *App) listDatasets(w http.ResponseWriter, r *http.Request) {
	ds, err := a.DB.ListDatasets(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

// uploadDataset lands a delivered file, validates it against its Source Schema,
// and lands the rows that coerce cleanly.
//
// Rows that fail are returned, not hidden: at 20 rows the rejects are as
// interesting as the successes, and at 100M they are the only thing anyone looks at.
func (a *App) uploadDataset(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	p, err := a.DB.GetProject(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ref := r.FormValue("schema_ref")
	actor := r.FormValue("actor")
	if actor == "" {
		actor = "unknown"
	}

	schemas, err := a.DB.ListSchemas(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var schema *SourceSchema
	for _, s := range schemas {
		if s.Ref() == ref {
			schema = s
			break
		}
	}
	if schema == nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project %s has no schema %q", p.Name, ref))
		return
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = hdr.Filename
	}

	a.runMu.Lock()
	ds, rejects, err := Ingest(a.Engine, a.Store, schema, name, raw)
	a.runMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	row := DatasetRow{
		Dataset: *ds, ID: newID("ds"), ProjectID: projectID,
		CreatedBy: actor, Rejects: rejects,
	}
	if row.Rejects == nil {
		row.Rejects = []Reject{}
	}
	if err := a.DB.PutDataset(r.Context(), row); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// datasetRows reads back the typed rows — the Dataset itself, which is what the
// rules actually see, rather than the raw text the file arrived as. Seeing the
// difference between the two is most of what makes a coercion bug obvious.
func (a *App) datasetRows(w http.ResponseWriter, r *http.Request) {
	ds, err := a.DB.ListDatasets(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var target *DatasetRow
	for i := range ds {
		if ds[i].ID == r.PathValue("did") {
			target = &ds[i]
			break
		}
	}
	if target == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no dataset %s", r.PathValue("did")))
		return
	}

	limit := 200
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}

	a.runMu.Lock()
	defer a.runMu.Unlock()
	rows, err := a.Engine.Query(fmt.Sprintf(
		"SELECT * FROM read_parquet('%s') ORDER BY __row LIMIT %d", target.DataPath, limit))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dataset": target,
		"rows":    rows,
	})
}
