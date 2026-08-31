package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

func (a *App) registerProjectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/migration-types", a.listMigrationTypes)
	mux.HandleFunc("GET /api/projects", a.listProjects)
	mux.HandleFunc("POST /api/projects", a.createProject)
	mux.HandleFunc("GET /api/projects/{id}", a.getProject)
}

type migrationTypeView struct {
	Name      string       `json:"name"`
	Title     string       `json:"title"`
	Schemas   []SchemaName `json:"schemas"`
	Transform string       `json:"transform"`
}

func (a *App) listMigrationTypes(w http.ResponseWriter, r *http.Request) {
	out := []migrationTypeView{}
	names := make([]string, 0, len(a.Types))
	for n := range a.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		mt := a.Types[n]
		v := migrationTypeView{Name: mt.Name, Title: mt.Title, Schemas: mt.SchemaNames()}
		if mt.Mapping.Transform != nil {
			v.Transform = mt.Mapping.Transform.Name
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := a.DB.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (a *App) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		MigrationType string `json:"migration_type"`
		Firm          string `json:"firm"`
		Actor         string `json:"actor"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	mt, err := a.Type(req.MigrationType)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Firm == "" {
		req.Firm = "PERSUS"
	}
	if req.Actor == "" {
		req.Actor = "unknown"
	}

	p := Project{
		ID: newID("prj"), Name: req.Name, MigrationType: mt.Name,
		Firm: req.Firm, CreatedBy: req.Actor,
	}
	if err := a.DB.CreateProject(r.Context(), p); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Choosing the Migration Type is what supplies the schemas: a new FAS
	// project already knows the Account Master before any data arrives.
	if err := a.Seed(r.Context(), p.ID, req.Actor, mt); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *App) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.DB.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func newID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
