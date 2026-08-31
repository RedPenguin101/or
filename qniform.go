package main

import (
	"fmt"
	"strings"
)

// TargetSystem names the system OnRamp migrates onto. There is exactly one, and
// naming it here rather than spelling the string at each use is what lets the
// spec store, its pin table and the vendored document agree on which target they
// are talking about.
const TargetSystem = "qniform"

// qniformLoadOrder ranks target endpoints by Qniform's own referential
// dependencies: an account cannot name a party that does not exist yet.
//
// This is fixed for every migration — a property of the target, not of any
// project — so it is recorded once here rather than declared per transform.
// Declaring a global constant per project is how it drifts.
var qniformLoadOrder = []string{
	"/securities",
	"/parties",
	"/party-relations",
	"/firms/{firm}/accounts",
	"/firms/{firm}/events/",
}

// loadRank returns where a target sits in Qniform's load order.
//
// An unranked target is an error rather than a default position: adding one
// should force a decision about what it depends on, not silently land last.
func loadRank(target string) (int, error) {
	parts := strings.Fields(target)
	if len(parts) != 2 {
		return 0, fmt.Errorf("target %q is not \"METHOD /path\"", target)
	}
	best, rank := -1, 0
	for i, prefix := range qniformLoadOrder {
		if strings.HasPrefix(parts[1], prefix) && len(prefix) > best {
			best, rank = len(prefix), i
		}
	}
	if best < 0 {
		return 0, fmt.Errorf(
			"target %q has no place in Qniform's load order; add it to qniformLoadOrder "+
				"once you know what it depends on", target)
	}
	return rank, nil
}
