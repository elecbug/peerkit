package p2p

import (
	"fmt"
	"sync"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

// forwardingProtocol creates per-message forwarding state. Protocol selection
// is centralized here so additional propagation algorithms can be added
// without spreading YAML-specific conditionals through the peer runtime.
type forwardingProtocol interface {
	name() string
	newMessageState() *forwardingState
}

type baseFloodingProtocol struct{}

func (baseFloodingProtocol) name() string {
	return protocols.BaseFlooding
}

func (baseFloodingProtocol) newMessageState() *forwardingState {
	return nil
}

type duplicateAwareFloodingProtocol struct{}

func (duplicateAwareFloodingProtocol) name() string {
	return protocols.DuplicateAwareFlooding
}

func (duplicateAwareFloodingProtocol) newMessageState() *forwardingState {
	return newForwardingState()
}

func selectForwardingProtocol(name string) (forwardingProtocol, error) {
	switch name {
	case "", protocols.BaseFlooding:
		return baseFloodingProtocol{}, nil
	case protocols.DuplicateAwareFlooding:
		return duplicateAwareFloodingProtocol{}, nil
	default:
		return nil, fmt.Errorf("unsupported forwarding protocol %q", name)
	}
}

// forwardingState tracks neighbors that demonstrably already hold a message.
// It is only allocated by duplicate_aware_flooding. The state is frozen when
// local processing completes, giving a precise boundary for suppression.
type forwardingState struct {
	mu      sync.Mutex
	pending bool
	holders map[string]struct{}
}

func newForwardingState() *forwardingState {
	return &forwardingState{
		pending: true,
		holders: make(map[string]struct{}),
	}
}

// observeDuplicate records a neighbor as already holding the message while
// forwarding is still pending. It returns true only for the first observation.
func (s *forwardingState) observeDuplicate(from string) bool {
	if s == nil || from == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending {
		return false
	}
	if _, exists := s.holders[from]; exists {
		return false
	}
	s.holders[from] = struct{}{}
	return true
}

// freeze closes the suppression window and returns a stable holder snapshot.
func (s *forwardingState) freeze() map[string]struct{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = false
	if len(s.holders) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(s.holders))
	for nodeID := range s.holders {
		result[nodeID] = struct{}{}
	}
	return result
}

func (s *forwardingState) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pending = false
	s.mu.Unlock()
}
