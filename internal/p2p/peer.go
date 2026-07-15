package p2p

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/metrics"
	"github.com/k-p2plab/peerkit/internal/model"
)

const ProtocolID = protocol.ID("/peerkit/eager-push/1.0.0")

type Peer struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cfg       *config.RuntimeNodeConfig
	host      host.Host
	writer    *metrics.Writer
	protocol  forwardingProtocol
	startedAt time.Time

	neighborsByNode map[string]config.RuntimeNeighborConfig
	nodeByPeerID    map[peer.ID]string
	senders         map[string]*edgeSender

	seenMu sync.Mutex
	seen   map[string]*forwardingState

	processQueue chan processItem
	sequence     atomic.Uint64
	httpServer   *http.Server
}

type processItem struct {
	message    WireMessage
	from       string
	enqueuedAt time.Time
	state      *forwardingState
}

type outboundItem struct {
	message    WireMessage
	enqueuedAt time.Time
}

type edgeSender struct {
	owner    *Peer
	neighbor config.RuntimeNeighborConfig
	queue    chan outboundItem
	rng      *rand.Rand
	streamMu sync.Mutex
	stream   network.Stream
}

type StatusResponse struct {
	NodeID             string   `json:"node_id"`
	PeerID             string   `json:"peer_id"`
	Protocol           string   `json:"protocol"`
	ConnectedNeighbors []string `json:"connected_neighbors"`
	ExpectedNeighbors  []string `json:"expected_neighbors"`
	ProcessQueueLength int      `json:"process_queue_length"`
}

type InjectRequest struct {
	Count            int   `json:"count"`
	IntervalMS       int64 `json:"interval_ms"`
	PayloadSizeBytes int   `json:"payload_size_bytes"`
}

type InjectResponse struct {
	Accepted int `json:"accepted"`
}

func New(ctx context.Context, cfg *config.RuntimeNodeConfig) (*Peer, error) {
	forwarding, err := selectForwardingProtocol(cfg.Protocol)
	if err != nil {
		return nil, err
	}
	cfg.Protocol = forwarding.name()

	privateKeyBytes, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	privateKey, err := crypto.UnmarshalPrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarshal private key: %w", err)
	}

	allowedIDs := make([]peer.ID, 0, len(cfg.Neighbors))
	neighborsByNode := make(map[string]config.RuntimeNeighborConfig, len(cfg.Neighbors))
	nodeByPeerID := make(map[peer.ID]string, len(cfg.Neighbors))
	for _, neighbor := range cfg.Neighbors {
		id, err := peer.Decode(neighbor.PeerID)
		if err != nil {
			return nil, fmt.Errorf("decode peer id for %s: %w", neighbor.NodeID, err)
		}
		allowedIDs = append(allowedIDs, id)
		neighborsByNode[neighbor.NodeID] = neighbor
		nodeByPeerID[id] = neighbor.NodeID
	}

	gater := NewStaticGater(allowedIDs)
	h, err := libp2p.New(
		libp2p.Identity(privateKey),
		libp2p.ListenAddrStrings(cfg.ListenAddress),
		libp2p.ConnectionGater(gater),
		libp2p.DisableRelay(),
		libp2p.DisableMetrics(),
		libp2p.Ping(false),
		libp2p.UserAgent("peerkit/0.3.0"),
	)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	writer, err := metrics.NewWriter(cfg.ResultFile)
	if err != nil {
		_ = h.Close()
		return nil, err
	}

	peerCtx, cancel := context.WithCancel(ctx)
	p := &Peer{
		ctx: peerCtx, cancel: cancel, cfg: cfg, host: h, writer: writer, protocol: forwarding,
		startedAt: time.Now(), neighborsByNode: neighborsByNode,
		nodeByPeerID: nodeByPeerID, senders: make(map[string]*edgeSender, len(cfg.Neighbors)),
		seen:         make(map[string]*forwardingState),
		processQueue: make(chan processItem, cfg.Performance.QueueCapacity),
	}

	p.host.SetStreamHandler(ProtocolID, p.handleStream)
	for _, neighbor := range cfg.Neighbors {
		id, _ := peer.Decode(neighbor.PeerID)
		address, err := multiaddr.NewMultiaddr(neighbor.Address)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("parse address for %s: %w", neighbor.NodeID, err)
		}
		p.host.Peerstore().AddAddrs(id, []multiaddr.Multiaddr{address}, peerstore.PermanentAddrTTL)
		sender := &edgeSender{
			owner: p, neighbor: neighbor,
			queue: make(chan outboundItem, neighbor.Network.QueueCapacity),
			rng:   rand.New(rand.NewSource(stableSeed(cfg.Seed, cfg.NodeID+"->"+neighbor.NodeID))),
		}
		p.senders[neighbor.NodeID] = sender
		go sender.run(peerCtx)
	}
	for i := 0; i < cfg.Performance.Workers; i++ {
		go p.runWorker(peerCtx, i)
	}

	p.record(metrics.Event{Type: "peer_started", PeerID: p.host.ID().String(), Expected: len(cfg.Neighbors)})
	return p, nil
}

func (p *Peer) StartHTTP() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", p.handleHealth)
	mux.HandleFunc("GET /v1/status", p.handleStatus)
	mux.HandleFunc("POST /v1/connect", p.handleConnect)
	mux.HandleFunc("POST /v1/prepare", p.handlePrepare)
	mux.HandleFunc("POST /v1/inject", p.handleInject)
	p.httpServer = &http.Server{
		Addr:              p.cfg.ControlAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := p.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control server failed: %v", err)
			p.cancel()
		}
	}()
	return nil
}

func (p *Peer) Wait() {
	<-p.ctx.Done()
}

func (p *Peer) Close() error {
	p.cancel()
	if p.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = p.httpServer.Shutdown(ctx)
		cancel()
	}
	for _, sender := range p.senders {
		sender.closeStream()
	}
	var errs []error
	if p.host != nil {
		errs = append(errs, p.host.Close())
	}
	if p.writer != nil {
		errs = append(errs, p.writer.Close())
	}
	return errors.Join(errs...)
}

func (p *Peer) ConnectNeighbors(ctx context.Context) error {
	var errs []error
	for _, neighbor := range p.cfg.Neighbors {
		if p.cfg.NodeIndex >= neighbor.Index {
			continue
		}
		id, err := peer.Decode(neighbor.PeerID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		address, err := multiaddr.NewMultiaddr(neighbor.Address)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if p.host.Network().Connectedness(id) == network.Connected {
			continue
		}
		var connectErr error
		for attempt := 0; attempt < 5; attempt++ {
			connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			connectErr = p.host.Connect(connectCtx, peer.AddrInfo{ID: id, Addrs: []multiaddr.Multiaddr{address}})
			cancel()
			if connectErr == nil {
				break
			}
			if !sleepContext(ctx, 500*time.Millisecond) {
				break
			}
		}
		if connectErr != nil {
			p.record(metrics.Event{Type: "connection_failed", To: neighbor.NodeID, Reason: connectErr.Error()})
			errs = append(errs, fmt.Errorf("connect to %s: %w", neighbor.NodeID, connectErr))
			continue
		}
		p.record(metrics.Event{Type: "connection_established", To: neighbor.NodeID})
	}
	return errors.Join(errs...)
}

func (p *Peer) PrepareStreams(ctx context.Context) error {
	var errs []error
	for _, sender := range p.senders {
		if err := sender.prepare(ctx); err != nil {
			errs = append(errs, fmt.Errorf("prepare stream to %s: %w", sender.neighbor.NodeID, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Peer) Status() StatusResponse {
	connectedSet := make(map[string]struct{})
	for _, id := range p.host.Network().Peers() {
		if node, ok := p.nodeByPeerID[id]; ok {
			connectedSet[node] = struct{}{}
		}
	}
	connected := make([]string, 0, len(connectedSet))
	for node := range connectedSet {
		connected = append(connected, node)
	}
	expected := make([]string, 0, len(p.cfg.Neighbors))
	for _, neighbor := range p.cfg.Neighbors {
		expected = append(expected, neighbor.NodeID)
	}
	sort.Strings(connected)
	sort.Strings(expected)
	return StatusResponse{
		NodeID: p.cfg.NodeID, PeerID: p.host.ID().String(), Protocol: p.cfg.Protocol,
		ConnectedNeighbors: connected, ExpectedNeighbors: expected,
		ProcessQueueLength: len(p.processQueue),
	}
}

func (p *Peer) Inject(count int, interval time.Duration, payloadBytes int) {
	for i := 0; i < count; i++ {
		if i > 0 && !sleepContext(p.ctx, interval) {
			return
		}
		sequence := p.sequence.Add(1)
		now := time.Now()
		message := WireMessage{
			ID:     fmt.Sprintf("%s-%d-%d", p.cfg.NodeID, now.UnixNano(), sequence),
			Origin: p.cfg.NodeID, Sequence: sequence, CreatedAtNS: now.UnixNano(),
			Hop: 0, PayloadBytes: payloadBytes,
		}
		state := p.protocol.newMessageState()
		p.seenMu.Lock()
		p.seen[message.ID] = state
		p.seenMu.Unlock()
		p.record(metrics.Event{
			Type: "message_created", MessageID: message.ID, Origin: message.Origin,
			Sequence: message.Sequence, Hop: message.Hop, PayloadBytes: message.PayloadBytes,
		})
		p.enqueueProcessing(processItem{message: message, enqueuedAt: now, state: state})
	}
}

func (p *Peer) handleStream(stream network.Stream) {
	defer stream.Close()
	remoteID := stream.Conn().RemotePeer()
	from, ok := p.nodeByPeerID[remoteID]
	if !ok {
		_ = stream.Reset()
		return
	}
	reader := bufio.NewReaderSize(stream, 64*1024)
	for {
		message, err := readFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				p.record(metrics.Event{Type: "stream_read_failed", From: from, Reason: err.Error()})
			}
			return
		}
		p.acceptMessage(message, from)
	}
}

func (p *Peer) acceptMessage(message WireMessage, from string) {
	now := time.Now()
	p.seenMu.Lock()
	state, duplicate := p.seen[message.ID]
	if !duplicate {
		state = p.protocol.newMessageState()
		p.seen[message.ID] = state
	}
	p.seenMu.Unlock()

	if duplicate && state != nil {
		state.observeDuplicate(from)
	}

	p.record(metrics.Event{
		Type: "message_received", MessageID: message.ID, Origin: message.Origin,
		From: from, Sequence: message.Sequence, Hop: message.Hop,
		PayloadBytes: message.PayloadBytes, Duplicate: duplicate,
	})
	if duplicate {
		return
	}
	p.enqueueProcessing(processItem{message: message, from: from, enqueuedAt: now, state: state})
}

func (p *Peer) enqueueProcessing(item processItem) {
	select {
	case p.processQueue <- item:
	case <-p.ctx.Done():
	default:
		item.state.close()
		p.record(metrics.Event{
			Type: "message_dropped", MessageID: item.message.ID, Origin: item.message.Origin,
			From: item.from, Sequence: item.message.Sequence, Hop: item.message.Hop,
			PayloadBytes: item.message.PayloadBytes, Reason: "node_queue_full",
		})
	}
}

func (p *Peer) runWorker(ctx context.Context, workerIndex int) {
	rng := rand.New(rand.NewSource(stableSeed(p.cfg.Seed, fmt.Sprintf("%s-worker-%d", p.cfg.NodeID, workerIndex))))
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-p.processQueue:
			queueWait := time.Since(item.enqueuedAt)
			processingDelay := model.SampleDuration(p.cfg.Performance.ProcessingDelay, rng)
			if !sleepContext(ctx, processingDelay) {
				return
			}
			suppressed := item.state.freeze()
			p.record(metrics.Event{
				Type: "message_processed", MessageID: item.message.ID, Origin: item.message.Origin,
				From: item.from, Sequence: item.message.Sequence, Hop: item.message.Hop,
				PayloadBytes: item.message.PayloadBytes, QueueWaitNS: queueWait.Nanoseconds(),
				ProcessingNS: processingDelay.Nanoseconds(),
			})
			for nodeID, sender := range p.senders {
				if nodeID == item.from {
					continue
				}
				if _, skip := suppressed[nodeID]; skip {
					p.record(metrics.Event{
						Type: "message_suppressed", MessageID: item.message.ID, Origin: item.message.Origin,
						To: nodeID, Sequence: item.message.Sequence, Hop: item.message.Hop + 1,
						PayloadBytes: item.message.PayloadBytes, Reason: "duplicate_neighbor",
					})
					continue
				}
				forward := item.message
				forward.Hop++
				sender.enqueue(forward)
			}
		}
	}
}

func (s *edgeSender) enqueue(message WireMessage) {
	item := outboundItem{message: message, enqueuedAt: time.Now()}
	select {
	case s.queue <- item:
	case <-s.owner.ctx.Done():
	default:
		s.owner.record(metrics.Event{
			Type: "message_dropped", MessageID: message.ID, Origin: message.Origin,
			To: s.neighbor.NodeID, Sequence: message.Sequence, Hop: message.Hop,
			PayloadBytes: message.PayloadBytes, Reason: "edge_queue_full",
		})
	}
}

func (s *edgeSender) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.closeStream()
			return
		case item := <-s.queue:
			s.send(ctx, item)
		}
	}
}

func (s *edgeSender) send(ctx context.Context, item outboundItem) {
	queueWait := time.Since(item.enqueuedAt)
	if s.neighbor.Network.LossRate > 0 && s.rng.Float64() < s.neighbor.Network.LossRate {
		s.owner.record(metrics.Event{
			Type: "message_dropped", MessageID: item.message.ID, Origin: item.message.Origin,
			To: s.neighbor.NodeID, Sequence: item.message.Sequence, Hop: item.message.Hop,
			PayloadBytes: item.message.PayloadBytes, QueueWaitNS: queueWait.Nanoseconds(),
			Reason: "emulated_loss", LossRate: s.neighbor.Network.LossRate,
		})
		return
	}

	propagationDelay := model.SampleDuration(s.neighbor.Network.Delay, s.rng)
	serializationDelay := model.SerializationDelay(item.message.PayloadBytes, s.neighbor.Network.BandwidthMbps)
	if !sleepContext(ctx, propagationDelay+serializationDelay) {
		return
	}

	if err := s.write(ctx, item.message); err != nil {
		s.owner.record(metrics.Event{
			Type: "message_dropped", MessageID: item.message.ID, Origin: item.message.Origin,
			To: s.neighbor.NodeID, Sequence: item.message.Sequence, Hop: item.message.Hop,
			PayloadBytes: item.message.PayloadBytes, QueueWaitNS: queueWait.Nanoseconds(),
			EdgeDelayNS: propagationDelay.Nanoseconds(), SerializationNS: serializationDelay.Nanoseconds(),
			Reason: "stream_write_failed: " + err.Error(),
		})
		return
	}
	s.owner.record(metrics.Event{
		Type: "message_sent", MessageID: item.message.ID, Origin: item.message.Origin,
		To: s.neighbor.NodeID, Sequence: item.message.Sequence, Hop: item.message.Hop,
		PayloadBytes: item.message.PayloadBytes, QueueWaitNS: queueWait.Nanoseconds(),
		EdgeDelayNS: propagationDelay.Nanoseconds(), SerializationNS: serializationDelay.Nanoseconds(),
	})
}

func (s *edgeSender) prepare(ctx context.Context) error {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	return s.ensureStreamLocked(ctx)
}

func (s *edgeSender) ensureStreamLocked(ctx context.Context) error {
	if s.stream != nil {
		return nil
	}
	id, err := peer.Decode(s.neighbor.PeerID)
	if err != nil {
		return err
	}
	streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stream, err := s.owner.host.NewStream(streamCtx, id, ProtocolID)
	if err != nil {
		return err
	}
	s.stream = stream
	return nil
}

func (s *edgeSender) write(ctx context.Context, message WireMessage) error {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	for attempt := 0; attempt < 2; attempt++ {
		if err := s.ensureStreamLocked(ctx); err != nil {
			return err
		}
		writer := bufio.NewWriterSize(s.stream, 64*1024)
		if err := writeFrame(writer, message); err == nil {
			return nil
		}
		_ = s.stream.Reset()
		s.stream = nil
	}
	return fmt.Errorf("write failed after stream reset")
}

func (s *edgeSender) closeStream() {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.stream != nil {
		_ = s.stream.Close()
		s.stream = nil
	}
}

func (p *Peer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "node_id": p.cfg.NodeID})
}

func (p *Peer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, p.Status())
}

func (p *Peer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if err := p.ConnectNeighbors(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "connected"})
}

func (p *Peer) handlePrepare(w http.ResponseWriter, r *http.Request) {
	if err := p.PrepareStreams(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "prepared"})
}

func (p *Peer) handleInject(w http.ResponseWriter, r *http.Request) {
	var request InjectRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.Count <= 0 || request.IntervalMS < 0 || request.PayloadSizeBytes < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid injection request"})
		return
	}
	go p.Inject(request.Count, time.Duration(request.IntervalMS)*time.Millisecond, request.PayloadSizeBytes)
	writeJSON(w, http.StatusAccepted, InjectResponse{Accepted: request.Count})
}

func (p *Peer) record(event metrics.Event) {
	event.TimestampNS = time.Now().UnixNano()
	event.RunID = p.cfg.RunID
	event.Experiment = p.cfg.ExperimentName
	event.Protocol = p.cfg.Protocol
	event.Node = p.cfg.NodeID
	if err := p.writer.Write(event); err != nil {
		log.Printf("write event: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stableSeed(base int64, text string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return base ^ int64(h.Sum64())
}
