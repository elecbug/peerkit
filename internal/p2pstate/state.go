package p2pstate

import "sync"

// State tracks which neighbors have already delivered a message before local
// processing completes. Those neighbors can be omitted from the eager
// forwarding fan-out because they demonstrably already have the message.
type State struct {
	mu             sync.Mutex
	receivedFrom   map[string]struct{}
	processingDone bool
}

func New(from string) *State {
	state := &State{receivedFrom: make(map[string]struct{})}
	if from != "" {
		state.receivedFrom[from] = struct{}{}
	}
	return state
}

// Observe records a duplicate sender only while the original copy is still
// queued or being processed. It returns true when the sender was newly added.
func (s *State) Observe(from string) bool {
	if from == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processingDone {
		return false
	}
	if _, exists := s.receivedFrom[from]; exists {
		return false
	}
	s.receivedFrom[from] = struct{}{}
	return true
}

// FinishProcessing establishes the exact suppression cutoff. A duplicate that
// wins this lock is suppressible; one arriving after it is counted as a
// duplicate but cannot change the already-decided forwarding set.
func (s *State) FinishProcessing() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processingDone = true
	result := make(map[string]struct{}, len(s.receivedFrom))
	for nodeID := range s.receivedFrom {
		result[nodeID] = struct{}{}
	}
	// Keep State itself as the seen marker, but release per-message neighbor
	// storage once forwarding has been decided.
	s.receivedFrom = nil
	return result
}
