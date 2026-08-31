package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// A Hash identifies content: the SHA-256 of a canonical serialisation,
// truncated, e.g. "sha256:a6088e2b58c1". Equal hashes mean equal content, which
// is what lets a Run be a pure function of its inputs and be reproduced from
// what it recorded (design.md §4).
//
// A defined type rather than an alias for string, so the compiler can tell a
// hash from the several other strings it sits beside — a schema's name, a
// transform input's alias, a target endpoint. Those all appeared as
// map[string]string, which said nothing about which half was which.
type Hash string

// hashJSON canonicalises through encoding/json, which sorts map keys, so the
// hash is stable across runs and across YAML formatting changes.
func hashJSON(v any) Hash {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("canonicalise: %v", err)) // struct types only; cannot fail
	}
	return hashBytes(b)
}

// hashBytes is for content that is already a byte sequence — a delivered file, a
// spec document — where the bytes as they arrived are the thing being identified
// and canonicalising would change what the hash means.
func hashBytes(b []byte) Hash {
	return Hash(fmt.Sprintf("sha256:%x", sha256.Sum256(b))[:19])
}
