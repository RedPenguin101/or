package main

import "testing"

func fld(t LogicalType, enc EncodingKind, scale int, nulls ...string) Field {
	return Field{Name: "F", Type: t, Encoding: Encoding{Kind: enc}, Scale: scale, NullValues: nulls}
}

func TestOverpunchTrailing(t *testing.T) {
	t.Parallel()
	f := fld(TypeDecimal, EncOverpunchTrailing, 2)
	cases := map[string]string{
		"1234K":   "-123.42", // K = -2
		"1234D":   "123.44",  // D = +4
		"0000{":   "0.00",    // { = +0
		"0000}":   "0.00",    // -0 must not render as "-0.00"
		"12345":   "123.45",  // unsigned trailing digit
		"000012C": "1.23",    // leading zeros stripped, scale applied
	}
	for in, want := range cases {
		got, err := f.Coerce(in)
		if err != nil {
			t.Errorf("Coerce(%q): unexpected error %v", in, err)
			continue
		}
		if got == nil || *got != want {
			t.Errorf("Coerce(%q) = %v, want %q", in, deref(got), want)
		}
	}
	if _, err := f.Coerce("12Z4K"); err == nil {
		t.Error("non-numeric body should fail coercion")
	}
	if _, err := f.Coerce("1234%"); err == nil {
		t.Error("invalid overpunch character should fail coercion")
	}
}

func TestCYYMMDD(t *testing.T) {
	t.Parallel()
	f := fld(TypeDate, EncCYYMMDD, 0, "0000000")
	cases := map[string]string{
		"0991231": "1999-12-31",
		"1260826": "2026-08-26",
		"0000101": "1900-01-01",
	}
	for in, want := range cases {
		got, err := f.Coerce(in)
		if err != nil || got == nil || *got != want {
			t.Errorf("Coerce(%q) = %v, %v; want %q", in, deref(got), err, want)
		}
	}
	// The declared sentinel is absent, not an error.
	got, err := f.Coerce("0000000")
	if err != nil || got != nil {
		t.Errorf("declared null sentinel should coerce to absent, got %v, %v", deref(got), err)
	}
	// An undeclared impossible date is an error, not silently absent.
	for _, bad := range []string{"0991331", "2260826", "099123", "0990230"} {
		if _, err := f.Coerce(bad); err == nil {
			t.Errorf("Coerce(%q) should fail", bad)
		}
	}
}

// A sentinel that is only a sentinel on *some* fields is the whole point of
// declaring them per-field: 0991231 is a real date above and absent here.
func TestNullSentinelIsPerField(t *testing.T) {
	t.Parallel()
	closeDt := fld(TypeDate, EncCYYMMDD, 0, "0000000", "0991231")
	got, err := closeDt.Coerce("0991231")
	if err != nil || got != nil {
		t.Errorf("CLOSE-DT sentinel should be absent, got %v, %v", deref(got), err)
	}
	openDt := fld(TypeDate, EncCYYMMDD, 0, "0000000")
	got, err = openDt.Coerce("0991231")
	if err != nil || got == nil || *got != "1999-12-31" {
		t.Errorf("OPEN-DT should read the same bytes as a real date, got %v, %v", deref(got), err)
	}
}

func TestYNAndEnum(t *testing.T) {
	t.Parallel()
	yn := fld(TypeBoolean, EncYN, 0, "", " ")
	for in, want := range map[string]string{"Y": "true", "N": "false"} {
		got, err := yn.Coerce(in)
		if err != nil || got == nil || *got != want {
			t.Errorf("Coerce(%q) = %v, %v; want %q", in, deref(got), err, want)
		}
	}
	if got, err := yn.Coerce(" "); err != nil || got != nil {
		t.Errorf("blank MARGIN-IND should be absent, got %v, %v", deref(got), err)
	}
	if _, err := yn.Coerce("X"); err == nil {
		t.Error("MARGIN-IND of X should fail rather than silently read false")
	}

	e := Field{Name: "ACCT-TYPE-CD", Type: TypeEnum, Required: true,
		Values: map[string]string{"I": "Individual", "H": "House"}}
	if _, err := e.Coerce("I"); err != nil {
		t.Errorf("declared enum value should coerce: %v", err)
	}
	if _, err := e.Coerce("Q"); err == nil {
		t.Error("undeclared enum value should fail coercion")
	}
}

func TestPackedDecimalIsRefused(t *testing.T) {
	t.Parallel()
	f := fld(TypeDecimal, EncPackedDecimal, 2)
	if _, err := f.Coerce("anything"); err == nil {
		t.Error("packed_decimal is reserved and must fail loudly rather than silently mis-read")
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
