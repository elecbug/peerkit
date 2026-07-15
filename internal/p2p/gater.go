package p2p

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*StaticGater)(nil)

type StaticGater struct {
	mu      sync.RWMutex
	allowed map[peer.ID]struct{}
}

func NewStaticGater(ids []peer.ID) *StaticGater {
	allowed := make(map[peer.ID]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	return &StaticGater{allowed: allowed}
}

func (g *StaticGater) IsAllowed(id peer.ID) bool {
	g.mu.RLock()
	_, ok := g.allowed[id]
	g.mu.RUnlock()
	return ok
}

func (g *StaticGater) InterceptPeerDial(id peer.ID) bool {
	return g.IsAllowed(id)
}

func (g *StaticGater) InterceptAddrDial(id peer.ID, _ ma.Multiaddr) bool {
	return g.IsAllowed(id)
}

func (g *StaticGater) InterceptAccept(_ network.ConnMultiaddrs) bool {
	// The remote peer ID is not authenticated at this stage.
	return true
}

func (g *StaticGater) InterceptSecured(_ network.Direction, id peer.ID, _ network.ConnMultiaddrs) bool {
	return g.IsAllowed(id)
}

func (g *StaticGater) InterceptUpgraded(conn network.Conn) (bool, control.DisconnectReason) {
	return g.IsAllowed(conn.RemotePeer()), 0
}
