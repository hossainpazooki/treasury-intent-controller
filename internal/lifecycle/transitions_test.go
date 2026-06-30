package lifecycle

import "testing"

// allStates is every state in the lifecycle graph, used to exhaustively probe
// forbidden edges.
var allStates = []State{
	Declared,
	Resolving,
	Active,
	Verifying,
	Achieved,
	Failed,
	FailedAtDispatch,
}

// permittedEdges enumerates EXACTLY the edges the contract permits. Anything not
// in this set must be forbidden.
var permittedEdges = []struct{ from, to State }{
	{Declared, Resolving},
	{Resolving, Active},
	{Resolving, Failed},
	{Active, Verifying},
	{Active, Failed},
	{Verifying, Achieved},
	{Verifying, Failed},
	{Verifying, FailedAtDispatch},
}

func isPermitted(from, to State) bool {
	for _, e := range permittedEdges {
		if e.from == from && e.to == to {
			return true
		}
	}
	return false
}

// TestPermittedEdgesAreValid asserts every permitted edge returns true.
func TestPermittedEdgesAreValid(t *testing.T) {
	for _, e := range permittedEdges {
		if !IsValidTransition(e.from, e.to) {
			t.Errorf("IsValidTransition(%q, %q) = false, want true (permitted edge)", e.from, e.to)
		}
	}
}

// TestForbiddenEdgesAreInvalid asserts every from->to pair NOT in the permitted
// set returns false. This is exhaustive over all state pairs, so it covers
// self-loops, backward edges, and skips.
func TestForbiddenEdgesAreInvalid(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			if isPermitted(from, to) {
				continue
			}
			if IsValidTransition(from, to) {
				t.Errorf("IsValidTransition(%q, %q) = true, want false (forbidden edge)", from, to)
			}
		}
	}
}

// TestTerminalsHaveNoOutgoingEdges asserts ACHIEVED, FAILED, and
// FAILED_AT_DISPATCH have no outgoing edge to any state.
func TestTerminalsHaveNoOutgoingEdges(t *testing.T) {
	terminals := []State{Achieved, Failed, FailedAtDispatch}
	for _, from := range terminals {
		if !from.IsTerminal() {
			t.Errorf("%q expected to be terminal", from)
		}
		for _, to := range allStates {
			if IsValidTransition(from, to) {
				t.Errorf("terminal %q has outgoing edge to %q, want none", from, to)
			}
		}
	}
}

// TestFailedAtDispatchReachableOnlyFromVerifying asserts FAILED_AT_DISPATCH is a
// valid destination ONLY from VERIFYING, and from no other source state.
func TestFailedAtDispatchReachableOnlyFromVerifying(t *testing.T) {
	if !IsValidTransition(Verifying, FailedAtDispatch) {
		t.Errorf("IsValidTransition(VERIFYING, FAILED_AT_DISPATCH) = false, want true")
	}
	for _, from := range allStates {
		if from == Verifying {
			continue
		}
		if IsValidTransition(from, FailedAtDispatch) {
			t.Errorf("IsValidTransition(%q, FAILED_AT_DISPATCH) = true, want false (only VERIFYING may reach it)", from)
		}
	}
}

// TestRepresentativeForbiddenEdges spot-checks named edges that must be false,
// guarding against an over-permissive table even if allStates is edited.
func TestRepresentativeForbiddenEdges(t *testing.T) {
	forbidden := []struct{ from, to State }{
		{Declared, Active},            // skip RESOLVING
		{Declared, Failed},            // DECLARED cannot fail directly
		{Resolving, Verifying},        // skip ACTIVE
		{Active, Achieved},            // skip VERIFYING
		{Active, FailedAtDispatch},    // not from ACTIVE
		{Verifying, Declared},         // backward
		{Declared, Declared},          // self-loop
		{Verifying, Resolving},        // backward
		{Resolving, FailedAtDispatch}, // not from RESOLVING
	}
	for _, e := range forbidden {
		if IsValidTransition(e.from, e.to) {
			t.Errorf("IsValidTransition(%q, %q) = true, want false", e.from, e.to)
		}
	}
}
