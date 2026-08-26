package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// App is the shared state every handler reads. Deliberately small: the system's
// substance is in the artifacts on disk (schemas, models, emitters) and the
// content-addressed data, not in server memory.
type App struct {
	DB     *DB
	Engine *Engine
	Spec   *Spec
	Store  string

	Types map[string]*MigrationType

	// One DuckDB connection is shared, and models materialise into fixed table
	// names, so runs are serialised. That is fine at iteration-1 volumes and is
	// the first thing to change when runs need to be concurrent — at which point
	// each run should get its own connection or its own schema.
	runMu sync.Mutex
}

// A MigrationType is one of the big legacy systems. Choosing it at project
// creation is what supplies the out-of-the-box schemas and transformation rules.
type MigrationType struct {
	Name    string                   `json:"name"`
	Title   string                   `json:"title"`
	Schemas map[string]*SourceSchema `json:"schemas"`
	Xform   *Xform                   `json:"-"`
}

var migrationTypeTitles = map[string]string{
	"FAS": "Fake Accounting System",
}

// LoadMigrationTypes reads every built-in Migration Type from disk at startup.
// Failing loudly here is deliberate: a malformed shipped schema is a build
// problem, and discovering it on first upload would be far worse.
func LoadMigrationTypes(root string) (map[string]*MigrationType, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := map[string]*MigrationType{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mt := &MigrationType{
			Name:    e.Name(),
			Title:   migrationTypeTitles[e.Name()],
			Schemas: map[string]*SourceSchema{},
		}
		if mt.Title == "" {
			mt.Title = e.Name()
		}
		paths, _ := filepath.Glob(filepath.Join(root, e.Name(), "schemas", "*.yaml"))
		for _, p := range paths {
			s, err := LoadSourceSchema(p)
			if err != nil {
				return nil, err
			}
			mt.Schemas[s.Ref()] = s
		}
		xdirs, _ := filepath.Glob(filepath.Join(root, e.Name(), "xforms", "*"))
		for _, d := range xdirs {
			x, err := LoadXform(d)
			if err != nil {
				return nil, err
			}
			mt.Xform = x // iteration 1 ships exactly one xform per Migration Type
		}
		out[e.Name()] = mt
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no migration types found under %s", root)
	}
	return out, nil
}

func (a *App) Type(name string) (*MigrationType, error) {
	mt, ok := a.Types[name]
	if !ok {
		return nil, fmt.Errorf("unknown migration type %q", name)
	}
	return mt, nil
}

func (mt *MigrationType) SchemaRefs() []string {
	out := make([]string, 0, len(mt.Schemas))
	for k := range mt.Schemas {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Seed pins a new project to the Migration Type's shipped schemas.
func (a *App) Seed(ctx context.Context, projectID string, mt *MigrationType) error {
	for _, ref := range mt.SchemaRefs() {
		if err := a.DB.PutSchema(ctx, projectID, mt.Schemas[ref]); err != nil {
			return err
		}
	}
	return nil
}
