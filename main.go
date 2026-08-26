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

	// The spec is optional to start but its absence is announced loudly: without
	// it, emitted payloads are unchecked and "Qniform-shaped" becomes a claim
	// rather than a fact.
	if spec, err := LoadSpec(qniformSpecPath()); err != nil {
		log.Printf("WARNING: Qniform spec not loaded (%v) — payloads will NOT be validated", err)
	} else {
		app.Spec = spec
		log.Printf("validating payloads against %s", qniformSpecPath())
	}

	for name, mt := range types {
		log.Printf("migration type %s: %d schema(s), xform %q", name, len(mt.Schemas), xformName(mt))
	}

	addr := envOr("ONRAMP_ADDR", ":8090")
	log.Printf("onramp listening on %s", addr)
	if err := http.ListenAndServe(addr, app.Routes()); err != nil {
		log.Fatal(err)
	}
}

func xformName(mt *MigrationType) string {
	if mt.Xform == nil {
		return "(none)"
	}
	return mt.Xform.Name
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
