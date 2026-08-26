package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Encodings are the physical layer: how a legacy system spells a value in text.
// They are deliberately separate from LogicalType (the semantic layer) so the
// type vocabulary stays small and target-facing while the encoding vocabulary
// grows per Migration Type. See devnotes/iteration1-artifacts.md §1.1.
type EncodingKind string

const (
	EncTrim              EncodingKind = "trim"
	EncCYYMMDD           EncodingKind = "cyymmdd"
	EncOverpunchTrailing EncodingKind = "overpunch_trailing"
	EncYN                EncodingKind = "yn"
	EncPackedDecimal     EncodingKind = "packed_decimal" // reserved: needs layout:fixed
)

type Encoding struct {
	Kind EncodingKind `yaml:"kind" json:"kind"`
}

// LogicalType is what the value means once decoded. Small on purpose.
type LogicalType string

const (
	TypeString  LogicalType = "string"
	TypeInteger LogicalType = "integer"
	TypeDecimal LogicalType = "decimal"
	TypeDate    LogicalType = "date"
	TypeBoolean LogicalType = "boolean"
	TypeEnum    LogicalType = "enum"
)

// overpunch maps the final character of a trailing-sign zoned decimal to its
// digit and sign. COBOL encodes both in one character: '{'..'I' are +0..+9 and
// '}'..'R' are -0..-9.
var overpunch = map[byte]struct {
	digit byte
	neg   bool
}{
	'{': {'0', false}, 'A': {'1', false}, 'B': {'2', false}, 'C': {'3', false}, 'D': {'4', false},
	'E': {'5', false}, 'F': {'6', false}, 'G': {'7', false}, 'H': {'8', false}, 'I': {'9', false},
	'}': {'0', true}, 'J': {'1', true}, 'K': {'2', true}, 'L': {'3', true}, 'M': {'4', true},
	'N': {'5', true}, 'O': {'6', true}, 'P': {'7', true}, 'Q': {'8', true}, 'R': {'9', true},
}

// CoercionError is a row-level reject at stage "coercion". It carries enough to
// show the user the offending cell, not just the fact that something failed.
type CoercionError struct {
	Field string
	Value string
	Msg   string
}

func (e *CoercionError) Error() string {
	return fmt.Sprintf("%s: cannot read %q: %s", e.Field, e.Value, e.Msg)
}

// Coerce turns one raw text cell into its typed form, returning the canonical
// string OnRamp carries downstream. Decimals stay strings throughout because
// Qniform's API takes decimal strings and a float round-trip would lose them.
//
// A nil return with nil error means the value matched a declared null sentinel:
// legacy systems have no NULL, only sentinels, and which sentinel means absent
// is field-specific knowledge that lives in the schema.
func (f Field) Coerce(raw string) (*string, error) {
	v := raw
	if f.Encoding.Kind == EncTrim || f.Encoding.Kind == "" {
		v = strings.TrimSpace(v)
	}

	for _, n := range f.NullValues {
		if v == n || raw == n {
			return nil, nil
		}
	}
	// An untrimmed all-space cell is absent in every legacy system we target,
	// whether or not the schema bothered to say so.
	if strings.TrimSpace(v) == "" && !f.Required {
		return nil, nil
	}

	fail := func(msg string) (*string, error) {
		return nil, &CoercionError{Field: f.Name, Value: raw, Msg: msg}
	}

	switch f.Encoding.Kind {
	case EncCYYMMDD:
		s, err := decodeCYYMMDD(v)
		if err != nil {
			return fail(err.Error())
		}
		return &s, nil
	case EncOverpunchTrailing:
		s, err := decodeOverpunch(v, f.Scale)
		if err != nil {
			return fail(err.Error())
		}
		return &s, nil
	case EncYN:
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "Y":
			s := "true"
			return &s, nil
		case "N":
			s := "false"
			return &s, nil
		default:
			return fail("expected Y or N")
		}
	case EncPackedDecimal:
		return fail("packed_decimal requires layout:fixed, not supported in iteration 1")
	}

	switch f.Type {
	case TypeInteger:
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return fail("not an integer")
		}
	case TypeDecimal:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fail("not a decimal")
		}
	case TypeDate:
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return fail("not an ISO date")
		}
	case TypeEnum:
		if _, ok := f.Values[v]; !ok {
			return fail(fmt.Sprintf("not one of the %d declared values", len(f.Values)))
		}
	}
	return &v, nil
}

// decodeCYYMMDD reads a 7-character date whose leading digit selects the
// century: 0 => 19xx, 1 => 20xx.
func decodeCYYMMDD(v string) (string, error) {
	if len(v) != 7 {
		return "", fmt.Errorf("expected 7 characters, got %d", len(v))
	}
	var century int
	switch v[0] {
	case '0':
		century = 1900
	case '1':
		century = 2000
	default:
		return "", fmt.Errorf("century indicator %q is not 0 or 1", v[0])
	}
	yy, err1 := strconv.Atoi(v[1:3])
	mm, err2 := strconv.Atoi(v[3:5])
	dd, err3 := strconv.Atoi(v[5:7])
	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("non-numeric date body")
	}
	t := time.Date(century+yy, time.Month(mm), dd, 0, 0, 0, 0, time.UTC)
	// time.Date normalises out-of-range values rather than failing, so compare
	// back to catch 0000000-style garbage and month 13.
	if t.Year() != century+yy || int(t.Month()) != mm || t.Day() != dd {
		return "", fmt.Errorf("not a real calendar date")
	}
	return t.Format("2006-01-02"), nil
}

// decodeOverpunch reads a zoned decimal whose final character carries the last
// digit and the sign, returning a plain decimal string at the field's scale.
func decodeOverpunch(v string, scale int) (string, error) {
	if v == "" {
		return "", fmt.Errorf("empty")
	}
	last := v[len(v)-1]
	digits := v[:len(v)-1]
	neg := false

	if op, ok := overpunch[last]; ok {
		digits += string(op.digit)
		neg = op.neg
	} else if last >= '0' && last <= '9' {
		digits += string(last) // unsigned, treated as positive
	} else {
		return "", fmt.Errorf("trailing character %q is not a valid sign overpunch", last)
	}

	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", fmt.Errorf("non-numeric body %q", digits)
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}

	out := digits
	if scale > 0 {
		for len(digits) <= scale {
			digits = "0" + digits
		}
		out = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out, nil
}
