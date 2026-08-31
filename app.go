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
// substance is in each project's own mapping and its content-addressed data, not
// in server memory. Types holds only the shipped catalogue new projects start
// from.
type App struct {
	DB     *DB
	Engine *Engine
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
	Name  string `json:"name"`
	Title string `json:"title"`

	// Mapping is the out-of-the-box mapping this Migration Type ships — the same
	// shape a project holds, so seeding is a copy rather than an assembly.
	Mapping *Mapping `json:"-"`
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
			Mapping: &Mapping{Schemas: map[SchemaName]*SourceSchema{}},
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
			mt.Mapping.Schemas[s.SchemaName] = s
		}
		tdirs, _ := filepath.Glob(filepath.Join(root, e.Name(), "transforms", "*"))
		for _, d := range tdirs {
			x, err := LoadTransform(d)
			if err != nil {
				return nil, err
			}
			mt.Mapping.Transform = x // iteration 1 ships exactly one transform per Migration Type
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

// SchemaNames lists the Migration Type's schemas in a stable order.
func (mt *MigrationType) SchemaNames() []SchemaName {
	out := make([]SchemaName, 0, len(mt.Mapping.Schemas))
	for k := range mt.Mapping.Schemas {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceSystem != out[j].SourceSystem {
			return out[i].SourceSystem < out[j].SourceSystem
		}
		return out[i].Schema < out[j].Schema
	})
	return out
}

// Seed pins a new project to the versions it starts from: the Migration Type's
// shipped schemas and transform, and the target contract as it stands today.
//
// This is the only moment the catalogue on disk is read on a project's behalf.
// Everything afterwards reads what was pinned here, which is what makes editing
// the catalogue a change to what new projects begin with rather than a silent
// change to what every existing one does on its next Run.
//
// The pins are the project's own, and they are the whole of what it shares with
// any other: two projects seeded from the same catalogue hold separate pins that
// happen to name the same immutable bodies.
func (a *App) Seed(ctx context.Context, projectID, actor string, mt *MigrationType) error {
	reason := fmt.Sprintf("seeded from the %s catalogue at project creation", mt.Name)
	m := mt.Mapping
	for _, n := range mt.SchemaNames() {
		if err := a.DB.PutSchema(ctx, projectID, actor, reason, m.Schemas[n]); err != nil {
			return err
		}
	}
	if m.Transform != nil {
		if err := a.DB.PutTransform(ctx, projectID, actor, reason, m.Transform); err != nil {
			return err
		}
	}
	return a.DB.PutSpec(ctx, projectID, actor, reason, m.Spec)
}
