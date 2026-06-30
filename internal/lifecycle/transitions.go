package lifecycle

// allowedTransitions is the lifecycle graph: the set of permitted from->to edges,
// and ONLY these. Terminal states (ACHIEVED, FAILED, FAILED_AT_DISPATCH) are absent
// as keys because they have no outgoing edges. FAILED_AT_DISPATCH appears as a
// destination ONLY under VERIFYING; the gate further restricts it to the
// dispatch-edge path (code-enforced).
//
// This table is used purely for membership lookups, so its map-iteration order
// never reaches the event log.
var allowedTransitions = map[State]map[State]bool{
	Declared:  {Resolving: true},
	Resolving: {Active: true, Failed: true},
	Active:    {Verifying: true, Failed: true},
	Verifying: {Achieved: true, Failed: true, FailedAtDispatch: true},
}

// IsValidTransition reports whether from->to is permitted by the lifecycle graph.
//
// Permitted edges (and ONLY these):
//
//	DECLARED  -> RESOLVING
//	RESOLVING -> ACTIVE, FAILED
//	ACTIVE    -> VERIFYING, FAILED
//	VERIFYING -> ACHIEVED, FAILED, FAILED_AT_DISPATCH
//
// Terminal states have no outgoing edges.
// FAILED_AT_DISPATCH is reachable ONLY from VERIFYING (table-enforced); the gate
// further restricts it to the dispatch-edge path (code-enforced).
func IsValidTransition(from, to State) bool {
	return allowedTransitions[from][to]
}
