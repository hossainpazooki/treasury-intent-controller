// Package lifecycle defines the intent authorization state machine: the set of
// states and the legal transitions between them.
package lifecycle

// State is a single node in the intent lifecycle graph.
type State string

const (
	Declared         State = "DECLARED"
	Resolving        State = "RESOLVING"
	Active           State = "ACTIVE"
	Verifying        State = "VERIFYING"
	Achieved         State = "ACHIEVED"
	Failed           State = "FAILED"
	FailedAtDispatch State = "FAILED_AT_DISPATCH"
)

// IsTerminal reports whether s is one of ACHIEVED, FAILED, FAILED_AT_DISPATCH.
// Terminal states have no outgoing edges.
func (s State) IsTerminal() bool {
	switch s {
	case Achieved, Failed, FailedAtDispatch:
		return true
	default:
		return false
	}
}
