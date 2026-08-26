package main

// A Reject is anything the pipeline withheld, at any point, for any reason.
//
// The three axes are deliberately separate because they have different owners:
// Class says whether one row is bad or a *set* is inconsistent, Phase says who is
// at fault (the extract, our rules, or Qniform), and Cause links a withheld row
// back to the upstream reject that caused it. See devnotes/design.md §3a.3 and
// devnotes/iteration1-artifacts.md §3.3.
type Reject struct {
	Class RejectClass `json:"class"`
	Phase RejectPhase `json:"phase"`

	Entity   string `json:"entity"`
	Identity string `json:"identity,omitempty"`
	Message  string `json:"message"`

	// Row-level detail.
	SourceRow int    `json:"source_row,omitempty"`
	Field     string `json:"field,omitempty"`
	Value     string `json:"value,omitempty"`

	// Set-level detail: one identity derived from rows that disagree.
	Values     []string `json:"values,omitempty"`
	SourceRows []int    `json:"source_rows,omitempty"`

	// Cause links a downstream withholding to the reject that produced it, so a
	// report reads "18 accounts withheld *because* one rep conflicts" rather
	// than "18 accounts failed".
	Cause *RejectRef `json:"cause,omitempty"`
}

type RejectRef struct {
	Output   string `json:"output"`
	Identity string `json:"identity"`
}

type RejectClass string

const (
	RejectRow      RejectClass = "row"      // this row is bad
	RejectConflict RejectClass = "conflict" // no row is bad; the set disagrees
)

// RejectPhase is *where* a withholding happened, which is a different question
// from *which output* it happened on. Naming it Phase rather than Stage keeps it
// distinct from the Outputs a Run produces.
type RejectPhase string

const (
	PhaseCoercion   RejectPhase = "coercion"   // the extract could not be read
	PhaseValidation RejectPhase = "validation" // our rules produced something invalid
	PhaseTarget     RejectPhase = "target"     // Qniform refused it
)
