package p2p

import (
	"reflect"
	"testing"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func TestHopWaveRelayCycle(t *testing.T) {
	cfg := protocols.HopWaveConfig{I: 3, F: 0.5}

	decision := decideHopWaveRelay(cfg, true, 0)
	if !decision.crest || decision.outgoingHop != 0 {
		t.Fatalf("source decision=%+v", decision)
	}

	decision = decideHopWaveRelay(cfg, false, 0)
	if decision.crest || decision.outgoingHop != 1 {
		t.Fatalf("first trough decision=%+v", decision)
	}

	decision = decideHopWaveRelay(cfg, false, 1)
	if decision.crest || decision.outgoingHop != 2 {
		t.Fatalf("second trough decision=%+v", decision)
	}

	decision = decideHopWaveRelay(cfg, false, 2)
	if !decision.crest || decision.outgoingHop != 0 {
		t.Fatalf("crest decision=%+v", decision)
	}
}

func TestHopWaveI1BehavesAsContinuousCrest(t *testing.T) {
	cfg := protocols.HopWaveConfig{I: 1, F: 0.5}
	for _, received := range []uint32{0, 1, 10} {
		decision := decideHopWaveRelay(cfg, false, received)
		if !decision.crest || decision.outgoingHop != 0 {
			t.Fatalf("received=%d decision=%+v", received, decision)
		}
	}
}

func TestSelectHopWaveTargetsUsesRoundedFractionAndIsStable(t *testing.T) {
	eligible := []string{"n4", "n2", "n1", "n3", "n0"}
	first := selectHopWaveTargets(eligible, 0.5, 42)
	second := selectHopWaveTargets([]string{"n0", "n1", "n2", "n3", "n4"}, 0.5, 42)
	if len(first) != 3 {
		t.Fatalf("selected=%d; want 3", len(first))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("selection depends on input order: %#v vs %#v", first, second)
	}
}
