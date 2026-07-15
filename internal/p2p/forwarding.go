package p2p

import (
	"github.com/k-p2plab/peerkit/internal/protocols"
)

type messageKnowledge struct {
	duplicateSenders  map[string]struct{}
	dontWantPeers     map[string]struct{}
	forwardingStarted bool
}

func newMessageKnowledge() *messageKnowledge {
	return &messageKnowledge{
		duplicateSenders: make(map[string]struct{}),
		dontWantPeers:    make(map[string]struct{}),
	}
}

func (p *Peer) registerLocalMessage(messageID string) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.seen[messageID] = struct{}{}
	if p.knowledge[messageID] == nil {
		p.knowledge[messageID] = newMessageKnowledge()
	}
}

func (p *Peer) registerReceivedMessage(messageID, from string) bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.knowledge[messageID]
	if state == nil {
		state = newMessageKnowledge()
		p.knowledge[messageID] = state
	}
	if _, duplicate := p.seen[messageID]; duplicate {
		if protocols.UsesDuplicateNeighborSuppression(p.cfg.Protocol) && !state.forwardingStarted {
			state.duplicateSenders[from] = struct{}{}
		}
		return true
	}
	p.seen[messageID] = struct{}{}
	return false
}

func (p *Peer) registerIDontWant(messageID, from string) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.knowledge[messageID]
	if state == nil {
		state = newMessageKnowledge()
		p.knowledge[messageID] = state
	}
	state.dontWantPeers[from] = struct{}{}
}

func (p *Peer) beginForwarding(messageID string) map[string]string {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.knowledge[messageID]
	if state == nil {
		state = newMessageKnowledge()
		p.knowledge[messageID] = state
	}
	state.forwardingStarted = true

	suppressed := make(map[string]string)
	switch protocols.Normalize(p.cfg.Protocol) {
	case protocols.DuplicateAwareFlooding:
		for nodeID := range state.duplicateSenders {
			suppressed[nodeID] = "duplicate_neighbor"
		}
	case protocols.IDontWantFlooding:
		for nodeID := range state.dontWantPeers {
			suppressed[nodeID] = "idontwant"
		}
	}
	return suppressed
}

func (p *Peer) shouldSuppressQueued(messageID, target string) bool {
	if !protocols.UsesIDontWant(p.cfg.Protocol) {
		return false
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.knowledge[messageID]
	if state == nil {
		return false
	}
	_, suppressed := state.dontWantPeers[target]
	return suppressed
}
