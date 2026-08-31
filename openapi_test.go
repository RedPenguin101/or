package main

import (
	"os"
	"testing"
)

func testSpec(t *testing.T) *Spec {
	t.Helper()
	path := targetSpecPath()
	if _, err := os.Stat(path); err != nil {
		// Deliberately fatal rather than skipped. A silently skipped spec check
		// is how "Qniform-shaped" output drifts into being a mock.
		t.Fatalf("target spec not found at %s: %v", path, err)
	}
	s, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	return s
}

func TestEmittedPayloadsMatchQniformSpec(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	run := runFAS(t)

	for _, o := range run.Outputs {
		rejects, err := spec.ValidateBatch(o.Target, o.Payloads)
		if err != nil {
			t.Fatalf("output %s: %v", o.Rule, err)
		}
		for _, r := range rejects {
			t.Errorf("output %s: %s %s: %s", o.Rule, r.Identity, r.Field, r.Message)
		}
	}
}

func TestSpecCatchesABadEnum(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	bad := NewOrderedMap()
	bad.Set("code", "X1")
	bad.Set("type", "chequing") // not one of Qniform's account types
	bad.Set("name", "X")
	rejects, err := spec.ValidateBatch("PUT /firms/{firm}/accounts", []*OrderedMap{bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(rejects) == 0 {
		t.Fatal("an invalid account type should be caught against the spec")
	}
}
