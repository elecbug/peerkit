package p2pstate

import "testing"

func TestStateSuppressesNeighborsObservedBeforeProcessingCompletes(t *testing.T) {
	state := New("parent")
	if !state.Observe("peer-b") {
		t.Fatal("expected peer-b to be newly observed")
	}
	if state.Observe("peer-b") {
		t.Fatal("duplicate observation should not be newly added")
	}
	suppressed := state.FinishProcessing()
	for _, nodeID := range []string{"parent", "peer-b"} {
		if _, ok := suppressed[nodeID]; !ok {
			t.Fatalf("expected %s in suppression set", nodeID)
		}
	}
}

func TestStateIgnoresNeighborsObservedAfterProcessingCompletes(t *testing.T) {
	state := New("parent")
	state.FinishProcessing()
	if state.Observe("late-peer") {
		t.Fatal("late duplicate must not alter forwarding decision")
	}
}
