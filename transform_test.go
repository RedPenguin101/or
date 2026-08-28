package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rule's SQL is sql/<rule name>.sql, by convention. The link cannot be
// misdeclared because it is not declared — but a rule with no file, or a file
// with no rule, still has to fail loudly. The second is the one that matters:
// an orphan file is where rule logic hides behind the label "implementation".
func TestSQLIsFoundByRuleName(t *testing.T) {
	t.Parallel()

	// A file belonging to no rule.
	dir := copyTransform(t)
	if err := os.WriteFile(filepath.Join(dir, "sql", "leftover.sql"),
		[]byte("SELECT 1 AS code, __row FROM account_master"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTransform(dir)
	if err == nil || !strings.Contains(err.Error(), "leftover") {
		t.Errorf("an orphan SQL file should fail to load, got: %v", err)
	}

	// A rule with no file.
	dir = copyTransform(t)
	if err := os.Remove(filepath.Join(dir, "sql", "reps.sql")); err != nil {
		t.Fatal(err)
	}
	_, err = LoadTransform(dir)
	if err == nil || !strings.Contains(err.Error(), "reps") {
		t.Errorf("a rule with no SQL should fail to load, got: %v", err)
	}
}

func copyTransform(t *testing.T) string {
	t.Helper()
	src := "migration_types/FAS/transforms/fas-accounts"
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "sql"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(src, "sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, "sql", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, "sql", e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(src, "transform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "transform.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}
