package p2p

import (
	"testing"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func TestDuplicateAwareProtocolSuppressesObservedNeighborBeforeFreeze(t *testing.T) {
	protocol, err := selectForwardingProtocol(protocols.DuplicateAwareFlooding)
	if err != nil {
		t.Fatal(err)
	}
	state := protocol.newMessageState()
	if !state.observeDuplicate("n2") {
		t.Fatal("first duplicate observation should be accepted")
	}
	if state.observeDuplicate("n2") {
		t.Fatal("duplicate observation should not be added twice")
	}
	suppressed := state.freeze()
	if _, ok := suppressed["n2"]; !ok {
		t.Fatal("n2 was not included in suppression snapshot")
	}
	if state.observeDuplicate("n3") {
		t.Fatal("observation after freeze must not affect forwarding")
	}
}

func TestBaseFloodingHasNoForwardingState(t *testing.T) {
	protocol, err := selectForwardingProtocol(protocols.BaseFlooding)
	if err != nil {
		t.Fatal(err)
	}
	if state := protocol.newMessageState(); state != nil {
		t.Fatal("base flooding must not allocate suppression state")
	}
}

func TestUnknownForwardingProtocolIsRejected(t *testing.T) {
	if _, err := selectForwardingProtocol("unknown"); err == nil {
		t.Fatal("expected unsupported protocol error")
	}
}
