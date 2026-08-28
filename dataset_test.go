package main

import (
	"os"
	"testing"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

func TestIngestFASAccountMaster(t *testing.T) {
	t.Parallel()
	schema, err := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	raw, err := os.ReadFile("testdata/fas_account_master.csv")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	ds, rejects, err := Ingest(eng, t.TempDir(), schema, "account_master", raw)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Row 20 (line 20 of the file) carries OPEN-DT 1231345 — month 13. It must be
	// withheld at coercion, and it must be the *only* row withheld.
	if len(rejects) != 1 {
		t.Fatalf("want 1 coercion reject, got %d: %+v", len(rejects), rejects)
	}
	r := rejects[0]
	if r.Class != RejectRow || r.Phase != PhaseCoercion {
		t.Errorf("reject class/phase = %s/%s, want row/coercion", r.Class, r.Phase)
	}
	if ds.Rows != 19 {
		t.Errorf("landed rows = %d, want 19", ds.Rows)
	}

	if err := eng.Attach("stg_account_master", ds.DataPath); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// Staging is typed, not text: prove the encodings actually decoded.
	got, err := eng.Query(`SELECT "ACCT-NO", "OPEN-DT", "CLOSE-DT", "CASH-BAL", "MARGIN-IND"
	                       FROM stg_account_master WHERE "ACCT-NO" = '0000100005'`)
	if err != nil || len(got) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(got))
	}
	row := got[0]
	if s := str(row["OPEN-DT"]); s != "2005-11-14" {
		t.Errorf("OPEN-DT = %q, want 2005-11-14 (cyymmdd 1051114)", s)
	}
	if row["CLOSE-DT"] != nil {
		t.Errorf("CLOSE-DT = %v, want nil (sentinel 0000000)", row["CLOSE-DT"])
	}
	if s := str(row["CASH-BAL"]); s != "-123.42" {
		t.Errorf("CASH-BAL = %q, want -123.42 (overpunch 0001234K: K is -2)", s)
	}
	if row["MARGIN-IND"] != false {
		t.Errorf("MARGIN-IND = %v, want false", row["MARGIN-IND"])
	}

	// The unassigned rep sentinel must land as absent, not as the string "0000".
	got, _ = eng.Query(`SELECT "REP-ID" FROM stg_account_master WHERE "ACCT-NO" = '0000100014'`)
	if len(got) != 1 || got[0]["REP-ID"] != nil {
		t.Errorf("REP-ID for unassigned account = %v, want nil", got[0]["REP-ID"])
	}
}

func TestIngestRejectsUnknownColumn(t *testing.T) {
	t.Parallel()
	schema, err := LoadSourceSchema("migration_types/FAS/schemas/account-master.yaml")
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	// A column the schema does not know about means the extract changed shape.
	// That is a schema problem and must fail loudly, not be silently dropped.
	_, _, err = Ingest(eng, t.TempDir(), schema, "x", []byte("ACCT-NO,SURPRISE\n1,2\n"))
	if err == nil {
		t.Fatal("unknown column should fail ingest")
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
