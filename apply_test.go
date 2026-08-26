package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func runFAS(t *testing.T) *Run {
	t.Helper()
	schema, err := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	x, err := LoadXform("migration_types/FAS/xforms/fas-accounts")
	if err != nil {
		t.Fatalf("xform: %v", err)
	}
	raw, err := os.ReadFile("testdata/fas_account_master.csv")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	ds, prior, err := Ingest(eng, t.TempDir(), schema, "account_master", raw)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	run, err := Apply(eng, ApplyInput{
		Project: "fas-demo", Tier: "full", Reason: "walking skeleton",
		Actor: Actor{Kind: "test", ID: "go"}, Firm: "PERSUS",
		Schemas:  map[string]*SourceSchema{"account_master": schema},
		Datasets: map[string]*Dataset{"account_master": ds},
		Xform:    x, Prior: prior, Spec: mustSpec(t),
	}, 1)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return run
}

// mustSpec loads Qniform's spec for every run in the suite: the skeleton is
// meant to prove real payloads, so a run that skips the check proves less.
func mustSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := LoadSpec(qniformSpecPath())
	if err != nil {
		t.Fatalf("Qniform spec (set QNIFORM_SPEC): %v", err)
	}
	return s
}

func output(r *Run, name string) *OutputResult {
	for i := range r.Outputs {
		if r.Outputs[i].Name == name {
			return &r.Outputs[i]
		}
	}
	return nil
}

func TestApplyFanOutAndCascade(t *testing.T) {
	t.Parallel()
	run := runFAS(t)

	// One flat file becomes two outputs, in dependency order.
	if got := len(run.Outputs); got != 2 {
		t.Fatalf("outputs = %d, want 2: %+v", got, run.Outputs)
	}
	if run.Outputs[0].Name != "parties" || run.Outputs[1].Name != "accounts" {
		t.Errorf("output order = %s, %s; accounts must follow parties",
			run.Outputs[0].Name, run.Outputs[1].Name)
	}
	// Coercion failures belong to the Run, not to an output: no rule saw them.
	if len(run.WithheldAtIngest) != 1 {
		t.Errorf("withheld at ingest = %d, want 1 (the month-13 date)", len(run.WithheldAtIngest))
	}

	// Two reps have inconsistent spellings across the file. Neither is a bad row;
	// the sets disagree, so both are quarantined and the rest still emit.
	p := output(run, "parties")
	if p.Rejected != 2 {
		t.Errorf("party rejects = %d, want 2 (REP 0042 and 0117)", p.Rejected)
	}
	for _, r := range p.Rejects {
		if r.Class != RejectConflict {
			t.Errorf("party reject %s has class %s, want conflict", r.Identity, r.Class)
		}
		if len(r.SourceRows) < 2 {
			t.Errorf("conflict %s must name the rows that disagree, got %v", r.Identity, r.SourceRows)
		}
	}
	if p.Accepted != 20 { // 16 customers + 4 clean reps/desks
		t.Errorf("parties accepted = %d, want 20", p.Accepted)
	}

	// The cascade: 2 quarantined reps withhold the 10 accounts that name them.
	a := output(run, "accounts")
	if a.Rejected != 10 {
		t.Errorf("account rejects = %d, want 10 (5 on REP 0042, 5 on REP 0117)", a.Rejected)
	}
	if a.Accepted != 9 {
		t.Errorf("accounts accepted = %d, want 9", a.Accepted)
	}
	// Every one must carry its cause, or the report says "10 accounts failed"
	// instead of "10 accounts failed *because* two reps conflict".
	for _, r := range a.Rejects {
		if r.Cause == nil {
			t.Errorf("account reject %s has no cause", r.Identity)
			continue
		}
		if r.Cause.Output != "parties" {
			t.Errorf("account reject %s blames output %q, want parties", r.Identity, r.Cause.Output)
		}
	}
	if run.Status() != "partial" {
		t.Errorf("status = %q, want partial: quarantine is not failure", run.Status())
	}
}

func TestApplyConditionalFieldsByType(t *testing.T) {
	t.Parallel()
	run := runFAS(t)
	a := output(run, "accounts")

	var house, customer int
	for _, p := range a.Payloads {
		typ, _ := p.Get("type")
		_, hasCounterparty := p.Get("counterparty")
		rp, _ := p.Get("responsible_party")

		switch typ {
		case "inventory":
			house++
			// Sending counterparty on an inventory account fails the WHOLE
			// request with a 400, not just the item. It must be absent.
			if hasCounterparty {
				t.Errorf("inventory account %v carries counterparty", mustGet(p, "code"))
			}
		case "customer":
			customer++
			if !hasCounterparty {
				code := mustGet(p, "code")
				// The one account with an unassigned rep is the exception.
				if code != "0000100014" {
					t.Errorf("customer account %v has no counterparty", code)
				}
			}
		default:
			t.Errorf("unexpected account type %v", typ)
		}
		_ = rp
	}
	if house != 3 {
		t.Errorf("inventory accounts = %d, want 3", house)
	}
}

func TestDerivedRolesVaryByAccountType(t *testing.T) {
	t.Parallel()
	run := runFAS(t)
	p := output(run, "parties")

	roles := map[string]int{}
	for _, pl := range p.Payloads {
		r, ok := pl.Get("roles")
		if !ok {
			t.Errorf("party %v has no roles", mustGet(pl, "code"))
			continue
		}
		om, ok := r.(*OrderedMap)
		if !ok || om.Len() != 1 {
			t.Errorf("party %v roles malformed: %#v", mustGet(pl, "code"), r)
			continue
		}
		roles[om.keys[0]]++
	}
	// The same REP-ID column yields a trader on house accounts and a rep on
	// customer accounts — one column, two target roles, decided by a sibling.
	if roles["trader"] != 3 {
		t.Errorf("trader parties = %d, want 3 (the three desks)", roles["trader"])
	}
	if roles["customer"] != 16 {
		t.Errorf("customer parties = %d, want 16", roles["customer"])
	}
	if roles["rep"] != 1 {
		t.Errorf("rep parties = %d, want 1 (only 0203 survives the conflicts)", roles["rep"])
	}
}

func TestRunIsDeterministic(t *testing.T) {
	t.Parallel()
	a, b := runFAS(t), runFAS(t)
	if a.Inputs.Triple != b.Inputs.Triple {
		t.Errorf("input triple differs between runs: %s vs %s", a.Inputs.Triple, b.Inputs.Triple)
	}
	// The payloads must be byte-identical, including order. Anything else means
	// something in the pipeline depends on scan order, which would break both
	// reproducibility and run diffing.
	ja, _ := json.Marshal(a.Outputs)
	jb, _ := json.Marshal(b.Outputs)
	if string(ja) != string(jb) {
		t.Error("two runs over the same inputs produced different output")
	}
	for name, n := range a.Nodes {
		if b.Nodes[name].Hash != n.Hash {
			t.Errorf("node %s hash differs between runs", name)
		}
	}
}

func mustGet(m *OrderedMap, k string) any { v, _ := m.Get(k); return v }

// The emitter can no longer rename or omit conditionally, so the only place a
// vocabulary could still accumulate is a rule quietly selecting columns that
// mean nothing to the target. This is the check that stops it.
func TestRuleColumnsMustBeTargetFields(t *testing.T) {
	t.Parallel()
	schema, err := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	if err != nil {
		t.Fatal(err)
	}
	x, err := LoadXform("migration_types/FAS/xforms/fas-accounts")
	if err != nil {
		t.Fatal(err)
	}
	// A column that is not a field of PUT /firms/{firm}/accounts, is not read
	// for a nesting key, and is not infrastructure.
	x.Models["accounts"] = strings.Replace(x.Models["accounts"],
		"    __row\nFROM account_master",
		"    \"STATUS-CD\" AS status,\n    __row\nFROM account_master", 1)
	if !strings.Contains(x.Models["accounts"], "AS status") {
		t.Fatal("test fixture did not apply; the model text must have changed")
	}

	raw, err := os.ReadFile("testdata/fas_account_master.csv")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	ds, prior, err := Ingest(eng, t.TempDir(), schema, "account_master", raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(eng, ApplyInput{
		Project: "fas-demo", Tier: "full", Reason: "guard", Actor: Actor{Kind: "test", ID: "go"},
		Firm:     "PERSUS",
		Schemas:  map[string]*SourceSchema{"account_master": schema},
		Datasets: map[string]*Dataset{"account_master": ds},
		Xform:    x, Prior: prior, Spec: mustSpec(t),
	}, 1)
	if err == nil {
		t.Fatal("a column that means nothing to the target must fail the run")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should name the offending column, got: %v", err)
	}
}
