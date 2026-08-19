package p2p

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

type hopWaveRelayDecision struct {
	crest       bool
	outgoingHop uint32
}

// decideHopWaveRelay implements the HopWave cycle described by the protocol:
// the publisher performs a Crest Relay with cycle hop 0; received messages
// perform Trough Relays until the received cycle hop reaches I-1, at which
// point the next relay is a Crest and the cycle hop is reset to 0.
func decideHopWaveRelay(cfg protocols.HopWaveConfig, source bool, receivedHop uint32) hopWaveRelayDecision {
	if source || cfg.I <= 1 || receivedHop >= uint32(cfg.I-1) {
		return hopWaveRelayDecision{crest: true, outgoingHop: 0}
	}
	return hopWaveRelayDecision{crest: false, outgoingHop: receivedHop + 1}
}

// selectHopWaveTargets chooses approximately F of the eligible neighbors.
// The candidate order is normalized before shuffling so that the same seed and
// message metadata produce the same subset regardless of topology slice order.
func selectHopWaveTargets(eligible []string, factor float64, seed int64) map[string]struct{} {
	selected := make(map[string]struct{})
	if len(eligible) == 0 || factor <= 0 {
		return selected
	}
	if factor >= 1 {
		for _, nodeID := range eligible {
			selected[nodeID] = struct{}{}
		}
		return selected
	}

	count := int(math.Round(factor * float64(len(eligible))))
	if count < 1 {
		count = 1
	}
	if count > len(eligible) {
		count = len(eligible)
	}

	candidates := append([]string(nil), eligible...)
	sort.Strings(candidates)
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	for _, nodeID := range candidates[:count] {
		selected[nodeID] = struct{}{}
	}
	return selected
}

func (p *Peer) hopWaveSelectionSeed(message WireMessage) int64 {
	return stableSeed(
		p.cfg.Seed,
		fmt.Sprintf("hopwave:%s:%s:%d:%d", p.cfg.NodeID, message.Origin, message.Sequence, message.HopWaveHop),
	)
}
