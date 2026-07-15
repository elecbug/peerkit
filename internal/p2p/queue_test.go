package p2p

import (
	"testing"

	"github.com/k-p2plab/peerkit/internal/config"
)

func TestDynamicEdgeQueueCapacityAndControlPriority(t *testing.T) {
	sender := &edgeSender{
		neighbor: config.RuntimeNeighborConfig{
			Network: config.ResolvedEdgeNetwork{QueueCapacity: 2},
		},
		wake: make(chan struct{}, 1),
	}
	data1 := outboundItem{frame: WireFrame{Type: frameTypeData, MessageID: "d1"}}
	data2 := outboundItem{frame: WireFrame{Type: frameTypeData, MessageID: "d2"}}
	data3 := outboundItem{frame: WireFrame{Type: frameTypeData, MessageID: "d3"}}
	control := outboundItem{frame: WireFrame{Type: frameTypeIDontWant, MessageID: "c1"}}

	if !sender.enqueue(data1, false) || !sender.enqueue(data2, false) {
		t.Fatal("data queue rejected entries below capacity")
	}
	if sender.enqueue(data3, false) {
		t.Fatal("data queue exceeded its configured capacity")
	}
	if !sender.enqueue(control, true) {
		t.Fatal("control queue unexpectedly rejected an entry")
	}

	first, ok := sender.dequeue()
	if !ok || first.frame.MessageID != "c1" {
		t.Fatalf("first dequeued frame=%q; want prioritized control frame", first.frame.MessageID)
	}
	second, _ := sender.dequeue()
	third, _ := sender.dequeue()
	if second.frame.MessageID != "d1" || third.frame.MessageID != "d2" {
		t.Fatalf("data FIFO order changed: %q, %q", second.frame.MessageID, third.frame.MessageID)
	}
	if _, ok := sender.dequeue(); ok {
		t.Fatal("queue should be empty")
	}
	if sender.dataQueue != nil || sender.controlQueue != nil {
		t.Fatal("empty dynamic queues retained backing slices")
	}
}
