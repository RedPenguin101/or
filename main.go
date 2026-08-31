package main

import (
	"context"
	"log"
	"net/http"
	"os"
)

func main() {
	ctx := context.Background()
	log.SetFlags(log.Ltime)

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := OpenDB(ctx, dsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	eng, err := NewEngine()
	if err != nil {
		log.Fatalf("duckdb: %v", err)
	}
	defer eng.Close()

	types, err := LoadMigrationTypes("migration_types")
	if err != nil {
		log.Fatalf("migration types: %v", err)
	}

	app := &App{DB: db, Engine: eng, Types: types, Store: envOr("ONRAMP_STORE", "data")}
	if err := os.MkdirAll(app.Store, 0o755); err != nil {
		log.Fatalf("store: %v", err)
	}

	// The seed copy of the target contract. Projects are pinned to a stored
	// version of it at creation and Runs read that; this is only what a new
	// project starts from. Still mandatory — a project that cannot be pinned to
	// a contract is one whose Runs could not be reproduced.
	spec, err := LoadSpec(targetSpecPath())
	if err != nil {
		log.Fatalf("target spec: %v", err)
	}
	// The shipped mapping is only complete once the target contract is attached:
	// what the source looks like, what the target looks like, and how to map
	// between them are one artefact, not two plus a global.
	for _, mt := range types {
		mt.Mapping.Spec = spec
	}
	log.Printf("seed target spec %s (%s)", targetSpecPath(), spec.Hash())

	for name, mt := range types {
		log.Printf("migration type %s: %d schema(s), transform %q", name, len(mt.Mapping.Schemas), transformName(mt))
	}

	addr := envOr("ONRAMP_ADDR", ":8090")
	log.Printf("onramp listening on %s", addr)
	if err := http.ListenAndServe(addr, app.Routes()); err != nil {
		log.Fatal(err)
	}
}

func transformName(mt *MigrationType) string {
	if mt.Mapping == nil || mt.Mapping.Transform == nil {
		return "(none)"
	}
	return mt.Mapping.Transform.Name
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
