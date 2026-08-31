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
	x, err := LoadTransform("migration_types/FAS/transforms/fas-accounts")
	if err != nil {
		t.Fatalf("xform: %v", err)
	}
	raw, err := os.ReadFile("testdata/fas_account_master.csv")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	store := t.TempDir()
	ds, _, err := Ingest(eng, store, schema, raw)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	run, err := ExecuteRun(eng, store, RunRequest{
		Number: 1, Project: "fas-demo", Reason: "walking skeleton",
		Actor: Actor{Kind: "test", ID: "go"},
		Mapping: &Mapping{
			Schemas:   map[SchemaName]*SourceSchema{schema.SchemaName: schema},
			Transform: x, Spec: mustSpec(t),
		},
		Datasets: map[string]*Dataset{"account_master": ds},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return run
}

// mustSpec loads Qniform's spec for every run in the suite: the skeleton is
// meant to prove real payloads, so a run that skips the check proves less.
func mustSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := LoadSpec(targetSpecPath())
	if err != nil {
		t.Fatalf("target spec: %v", err)
	}
	return s
}

// output finds one rule's emission in the Run.
func output(r *Run, rule string) *RuleOutput {
	for i := range r.Outputs {
		if r.Outputs[i].Rule == rule {
			return &r.Outputs[i]
		}
	}
	return nil
}

func TestApplyFanOutAndCascade(t *testing.T) {
	t.Parallel()
	run := runFAS(t)

	// One flat file becomes three rule emissions across two endpoints, ordered by
	// Qniform's own load order: parties before the accounts that name them.
	if got := len(run.Outputs); got != 3 {
		t.Fatalf("outputs = %d, want 3: %+v", got, run.Outputs)
	}
	var order []string
	for _, o := range run.Outputs {
		order = append(order, o.Rule)
	}
	if strings.Join(order, ",") != "counterparties,reps,accounts" {
		t.Errorf("rule order = %v; accounts must run after both party rules", order)
	}
	// Two reps have inconsistent spellings across the file. Neither is a bad row;
	// the sets disagree, so both are quarantined and the rest still emit.
	p := output(run, "reps")
	if p.Rejected != 2 {
		t.Errorf("rep rejects = %d, want 2 (REP 0042 and 0117)", p.Rejected)
	}
	for _, r := range p.Rejects {
		if r.Class != RejectConflict {
			t.Errorf("party reject %s has class %s, want conflict", r.Identity, r.Class)
		}
		if len(r.SourceRows) < 2 {
			t.Errorf("conflict %s must name the rows that disagree, got %v", r.Identity, r.SourceRows)
		}
	}
	if p.Accepted != 4 {
		t.Errorf("reps accepted = %d, want 4 (0203 plus three desks)", p.Accepted)
	}
	// 16 customers, one custodian and two program banks: the counterparty rule
	// spans three ledgers, because the other side of the relationship is a
	// different kind of party on each.
	if c := output(run, "counterparties"); c.Accepted != 19 {
		t.Errorf("counterparties accepted = %d, want 19", c.Accepted)
	}

	// The cascade: 2 quarantined reps withhold the 10 accounts that name them.
	// The eleventh reject is the account whose number falls in no known ledger,
	// which is a different failure and carries no cause.
	a := output(run, "accounts")
	if a.Rejected != 11 {
		t.Errorf("account rejects = %d, want 11 (5 on REP 0042, 5 on REP 0117, 1 unmapped ledger)", a.Rejected)
	}
	if a.Accepted != 15 {
		t.Errorf("accounts accepted = %d, want 15", a.Accepted)
	}
	// Every cascaded one must carry its cause, or the report says "10 accounts
	// failed" instead of "10 accounts failed *because* two reps conflict".
	var cascaded, unmapped int
	for _, r := range a.Rejects {
		if r.Cause == nil {
			unmapped++
			if r.Identity != "0097000001" || r.Field != "type" {
				t.Errorf("uncaused account reject %s on %q; want the unmapped-ledger account", r.Identity, r.Field)
			}
			continue
		}
		cascaded++
		if r.Cause.Output != "reps" {
			t.Errorf("account reject %s blames %q, want reps", r.Identity, r.Cause.Output)
		}
	}
	if cascaded != 10 || unmapped != 1 {
		t.Errorf("account rejects = %d cascaded + %d unmapped, want 10 + 1", cascaded, unmapped)
	}
	// Quarantine is not failure: the run withholds what conflicts and emits the
	// rest, so both counts are non-zero.
	if run.Totals.Accepted == 0 || run.Totals.Rejected == 0 {
		t.Errorf("totals = %+v, want both non-zero: quarantine is partial, not failed", run.Totals)
	}
}

// The Qniform account type is read from the FAS account number range, not from
// ACCT-TYPE-CD — and the type then decides which optional slots may be filled.
// Qniform is strict in both directions: filling a slot the type forbids fails
// the whole request rather than the item, so the rule has to get it right here.
func TestAccountTypeComesFromTheNumberRange(t *testing.T) {
	t.Parallel()
	run := runFAS(t)
	a := output(run, "accounts")

	// What the ledger map says each of these accounts is. Three FAS ledgers
	// collapse onto `control`, so the mapping is not a bijection.
	wantType := map[string]string{
		"0000010001": "bank",      // program bank ledger
		"0000010002": "bank",      //
		"0000100009": "customer",  // customer ledger
		"0000100014": "customer",  //
		"0091000001": "inventory", // firm proprietary ledger
		"0093000001": "control",   // clearing / suspense
		"0093900001": "custody",   // broker safekeeping
		"0095000001": "control",   // dividend and interest
		"0099999999": "control",   // firm control
	}

	byType := map[string]int{}
	for _, p := range a.Payloads {
		code, _ := mustGet(p, "code").(string)
		typ, _ := mustGet(p, "type").(string)
		byType[typ]++
		if want, ok := wantType[code]; ok && typ != want {
			t.Errorf("account %s type = %q, want %q from its number range", code, typ, want)
		}

		_, hasCounterparty := p.Get("counterparty")
		_, hasResponsible := p.Get("responsible_party")

		switch typ {
		case "customer":
			// Optional, but every one of these records a named holder.
			if !hasCounterparty {
				t.Errorf("customer account %s has no counterparty", code)
			}
		case "custody", "bank":
			// Required: the custodian holding the stock, the bank taking the
			// deposit. And nobody is answerable for either in Qniform's sense.
			if !hasCounterparty {
				t.Errorf("%s account %s must name a counterparty", typ, code)
			}
			if hasResponsible {
				t.Errorf("%s account %s carries a responsible party, which is rejected", typ, code)
			}
		case "inventory":
			// Rejected: the firm is both sides of its own positions.
			if hasCounterparty {
				t.Errorf("inventory account %s carries a counterparty", code)
			}
		case "control":
			if hasCounterparty || hasResponsible {
				t.Errorf("control account %s carries a counterparty or responsible party", code)
			}
		default:
			t.Errorf("account %s has unexpected type %q", code, typ)
		}
	}

	// Six of Qniform's seven account types, reached from one flat file with no
	// column saying which is which.
	want := map[string]int{"bank": 2, "customer": 6, "inventory": 3, "control": 3, "custody": 1}
	for typ, n := range want {
		if byType[typ] != n {
			t.Errorf("%s accounts = %d, want %d", typ, byType[typ], n)
		}
	}
}

func TestDerivedRolesVaryByAccountType(t *testing.T) {
	t.Parallel()
	run := runFAS(t)
	roles := map[string]int{}
	var all []*OrderedMap
	for _, o := range run.Outputs {
		if o.Target == "PUT /parties" {
			all = append(all, o.Payloads...)
		}
	}
	for _, pl := range all {
		r, ok := pl.Get("roles")
		if !ok {
			t.Errorf("party %v has no roles", mustGet(pl, "code"))
			continue
		}
		key, ok := singleKey(r)
		if !ok {
			t.Errorf("party %v roles malformed: %#v", mustGet(pl, "code"), r)
			continue
		}
		roles[key]++
	}
	// Two columns, five roles, every one of them decided by the ledger the
	// account number falls in. REP-ID is a trader on the firm's proprietary
	// ledger and a rep on the customer ledger; CUST-NAME-1 is a customer, a
	// custodian or a bank depending on which ledger names it.
	want := map[string]int{
		"trader":    3,  // the three desks
		"rep":       1,  // only 0203 survives the name conflicts
		"customer":  16, // one per customer-ledger account with a holder
		"custodian": 1,  // the depository, from the broker safekeeping ledger
		"bank":      2,  // the two sweep program banks
	}
	for role, n := range want {
		if roles[role] != n {
			t.Errorf("%s parties = %d, want %d", role, roles[role], n)
		}
	}
	for role, n := range roles {
		if _, ok := want[role]; !ok {
			t.Errorf("unexpected role %q on %d parties", role, n)
		}
	}
}

func TestRunIsDeterministic(t *testing.T) {
	t.Parallel()
	a, b := runFAS(t), runFAS(t)
	if a.Inputs.Digest != b.Inputs.Digest {
		t.Errorf("input digest differs between runs: %s vs %s", a.Inputs.Digest, b.Inputs.Digest)
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

// singleKey reads the one key from a roles object. A rule whose role never
// varies states it as a literal map; one whose role varies by row builds an
// OrderedMap from a column. Both are one-key objects to Qniform.
func singleKey(v any) (string, bool) {
	switch t := v.(type) {
	case *OrderedMap:
		if t.Len() == 1 {
			return t.keys[0], true
		}
	case map[string]any:
		if len(t) == 1 {
			for k := range t {
				return k, true
			}
		}
	}
	return "", false
}

// The emitter can no longer rename or omit conditionally, so the only place a
// vocabulary could still accumulate is a rule quietly selecting columns that
// mean nothing to the target. This is the check that stops it.
func TestRuleColumnsMustBeTargetFields(t *testing.T) {
	t.Parallel()
	schema, err := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	if err != nil {
		t.Fatal(err)
	}
	x, err := LoadTransform("migration_types/FAS/transforms/fas-accounts")
	if err != nil {
		t.Fatal(err)
	}
	// A column that is not a field of PUT /firms/{firm}/accounts, is not read
	// for a nesting key, and is not infrastructure.
	x.SQL["accounts"] = strings.Replace(x.SQL["accounts"],
		"    __row\nFROM classified",
		"    \"STATUS-CD\" AS status,\n    __row\nFROM classified", 1)
	if !strings.Contains(x.SQL["accounts"], "AS status") {
		t.Fatal("test fixture did not apply; the SQL text must have changed")
	}

	raw, err := os.ReadFile("testdata/fas_account_master.csv")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	store := t.TempDir()
	ds, _, err := Ingest(eng, store, schema, raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteRun(eng, store, RunRequest{
		Number: 1, Project: "fas-demo", Reason: "guard", Actor: Actor{Kind: "test", ID: "go"},
		Mapping: &Mapping{
			Schemas:   map[SchemaName]*SourceSchema{schema.SchemaName: schema},
			Transform: x, Spec: mustSpec(t),
		},
		Datasets: map[string]*Dataset{"account_master": ds},
	})
	if err == nil {
		t.Fatal("a column that means nothing to the target must fail the run")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should name the offending column, got: %v", err)
	}
}

// The target's contract is an input, not a dependency: validating against it
// withholds payloads, so a changed contract changes the output. If it were left
// out of the recorded inputs, two runs could share a digest and disagree.
func TestTargetSpecIsPartOfTheContract(t *testing.T) {
	t.Parallel()
	base := runFAS(t)
	if len(base.Inputs.Mapping.Specs) != 2 {
		t.Fatalf("recorded specs = %v, want one per target written", base.Inputs.Mapping.Specs)
	}
	for _, target := range []string{"PUT /parties", "PUT /firms/{firm}/accounts"} {
		if base.Inputs.Mapping.Specs[target] == "" {
			t.Errorf("no contract recorded for %s", target)
		}
	}

	// Narrow what Qniform accepts, exactly as a Qniform release could.
	spec := mustSpec(t)
	item, err := spec.Operation("PUT /parties")
	if err != nil {
		t.Fatal(err)
	}
	if item.Type == "array" {
		item = item.Items
	}
	item.Required = append(item.Required, "pre_name")

	schema, _ := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	x, _ := LoadTransform("migration_types/FAS/transforms/fas-accounts")
	raw, _ := os.ReadFile("testdata/fas_account_master.csv")
	eng := testEngine(t)
	store := t.TempDir()
	ds, _, err := Ingest(eng, store, schema, raw)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ExecuteRun(eng, store, RunRequest{
		Number: 1, Project: "fas-demo", Reason: "narrowed contract",
		Actor: Actor{Kind: "test", ID: "go"},
		Mapping: &Mapping{
			Schemas:   map[SchemaName]*SourceSchema{schema.SchemaName: schema},
			Transform: x, Spec: spec,
		},
		Datasets: map[string]*Dataset{"account_master": ds},
	})
	if err != nil {
		t.Fatal(err)
	}

	if changed.Inputs.Mapping.Specs["PUT /parties"] == base.Inputs.Mapping.Specs["PUT /parties"] {
		t.Error("a narrowed contract must change that target's recorded version")
	}
	if changed.Inputs.Digest == base.Inputs.Digest {
		t.Error("a narrowed contract must change the Run's digest — otherwise two runs " +
			"can share an identity and disagree about what they produced")
	}
	// The other target is untouched, which is why specs are hashed per target
	// rather than over the whole document.
	if changed.Inputs.Mapping.Specs["PUT /firms/{firm}/accounts"] != base.Inputs.Mapping.Specs["PUT /firms/{firm}/accounts"] {
		t.Error("an unrelated endpoint's contract should not have changed")
	}
}
