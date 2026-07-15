package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/metrics"
	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

type SwarmRunStatus struct {
	State      string              `json:"state"`
	Registered int                 `json:"registered"`
	Expected   int                 `json:"expected"`
	Error      string              `json:"error,omitempty"`
	Summary    *metrics.RunSummary `json:"summary,omitempty"`
}

type swarmCoordinator struct {
	ctx       context.Context
	scenario  *config.Scenario
	runID     string
	resultDir string

	mu            sync.RWMutex
	registrations map[int]peerkitp2p.SwarmRegisterRequest
	runtime       map[int]config.RuntimeNodeConfig
	endpoints     controlEndpoints
	state         string
	runErr        string
	summary       *metrics.RunSummary
	changed       chan struct{}
	done          chan struct{}
}

func newSwarmCoordinator(ctx context.Context, scenario *config.Scenario, runID, resultDir string) (*swarmCoordinator, error) {
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return nil, err
	}
	coordinator := &swarmCoordinator{
		ctx:           ctx,
		scenario:      scenario,
		runID:         runID,
		resultDir:     resultDir,
		registrations: make(map[int]peerkitp2p.SwarmRegisterRequest, len(scenario.Topology.Nodes)),
		runtime:       make(map[int]config.RuntimeNodeConfig, len(scenario.Topology.Nodes)),
		endpoints:     make(controlEndpoints, len(scenario.Topology.Nodes)),
		state:         "waiting_for_peers",
		changed:       make(chan struct{}, 1),
		done:          make(chan struct{}),
	}
	return coordinator, nil
}

func (c *swarmCoordinator) start() {
	go c.run()
}

func (c *swarmCoordinator) run() {
	defer close(c.done)
	startupTimeout := time.Duration(c.scenario.Deployment.Swarm.StartupTimeoutSeconds) * time.Second
	startupCtx, cancel := context.WithTimeout(c.ctx, startupTimeout)
	defer cancel()

	log.Printf("waiting for %d Swarm peer tasks to register", len(c.scenario.Topology.Nodes))
	if err := c.waitForRegistrations(startupCtx); err != nil {
		c.fail(err)
		return
	}
	log.Printf("all Swarm peer tasks registered")
	if err := c.buildRuntimeConfigs(); err != nil {
		c.fail(err)
		return
	}
	c.setState("waiting_for_peer_readiness")

	summary, err := executeExperiment(
		c.ctx,
		c.scenario,
		c.endpointSnapshot(),
		c.resultDir,
		c.resultDir,
		experimentExecutionOptions{
			ReadyTimeout: startupTimeout,
			Download:     true,
		},
	)
	if err != nil {
		c.fail(err)
		return
	}
	log.Printf("Swarm experiment completed")
	c.mu.Lock()
	c.summary = summary
	c.state = "completed"
	c.mu.Unlock()
}

func (c *swarmCoordinator) waitForRegistrations(ctx context.Context) error {
	for {
		c.mu.RLock()
		count := len(c.registrations)
		expected := len(c.scenario.Topology.Nodes)
		c.mu.RUnlock()
		if count == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("only %d/%d swarm peers registered: %w", count, expected, ctx.Err())
		case <-c.changed:
		}
	}
}

func (c *swarmCoordinator) register(request peerkitp2p.SwarmRegisterRequest) error {
	if request.Slot < 1 || request.Slot > len(c.scenario.Topology.Nodes) {
		return fmt.Errorf("slot %d is outside 1..%d", request.Slot, len(c.scenario.Topology.Nodes))
	}
	if _, err := peer.Decode(request.PeerID); err != nil {
		return fmt.Errorf("invalid peer id: %w", err)
	}
	if _, err := multiaddr.NewMultiaddr(request.Address); err != nil {
		return fmt.Errorf("invalid libp2p address: %w", err)
	}
	if request.ControlURL == "" {
		return fmt.Errorf("control_url is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != "waiting_for_peers" {
		existing, ok := c.registrations[request.Slot]
		if ok && existing.PeerID == request.PeerID {
			return nil
		}
		return fmt.Errorf("peer registration is closed in state %s", c.state)
	}
	c.registrations[request.Slot] = request
	select {
	case c.changed <- struct{}{}:
	default:
	}
	return nil
}

func (c *swarmCoordinator) buildRuntimeConfigs() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.registrations) != len(c.scenario.Topology.Nodes) {
		return fmt.Errorf("cannot build runtime configs before all peers register")
	}

	byNode := make(map[string]peerkitp2p.SwarmRegisterRequest, len(c.registrations))
	indexByNode := make(map[string]int, len(c.scenario.Topology.Nodes))
	for index, node := range c.scenario.Topology.Nodes {
		registration := c.registrations[index+1]
		byNode[node.ID] = registration
		indexByNode[node.ID] = index
		c.endpoints[node.ID] = registration.ControlURL
	}

	neighbors := make(map[string][]config.RuntimeNeighborConfig, len(c.scenario.Topology.Nodes))
	for _, edge := range c.scenario.Topology.Edges {
		source := byNode[edge.Source]
		target := byNode[edge.Target]
		network := edge.Network.Resolve()
		neighbors[edge.Source] = append(neighbors[edge.Source], config.RuntimeNeighborConfig{
			NodeID:  edge.Target,
			Index:   indexByNode[edge.Target],
			PeerID:  target.PeerID,
			Address: target.Address,
			Network: network,
		})
		neighbors[edge.Target] = append(neighbors[edge.Target], config.RuntimeNeighborConfig{
			NodeID:  edge.Source,
			Index:   indexByNode[edge.Source],
			PeerID:  source.PeerID,
			Address: source.Address,
			Network: network,
		})
	}

	for index, node := range c.scenario.Topology.Nodes {
		sort.Slice(neighbors[node.ID], func(i, j int) bool {
			return neighbors[node.ID][i].Index < neighbors[node.ID][j].Index
		})
		c.runtime[index+1] = config.RuntimeNodeConfig{
			RunID:          c.runID,
			ExperimentName: c.scenario.Experiment.Name,
			Protocol:       c.scenario.Protocol,
			NodeID:         node.ID,
			NodeIndex:      index,
			Seed:           c.scenario.Experiment.Seed + int64(index)*1_000_003,
			ListenAddress:  "/ip4/0.0.0.0/tcp/4001",
			ControlAddress: ":8080",
			ResultFile:     filepath.Join("/tmp/peerkit-results", node.ID+".jsonl"),
			Metrics:        c.scenario.Metrics,
			Performance:    *node.Performance,
			Neighbors:      neighbors[node.ID],
		}
	}
	c.state = "config_ready"
	return nil
}

func (c *swarmCoordinator) runtimeConfig(slot int) (config.RuntimeNodeConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.runtime[slot]
	return value, ok
}

func (c *swarmCoordinator) endpointSnapshot() controlEndpoints {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneEndpoints(c.endpoints)
}

func (c *swarmCoordinator) status() SwarmRunStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return SwarmRunStatus{
		State:      c.state,
		Registered: len(c.registrations),
		Expected:   len(c.scenario.Topology.Nodes),
		Error:      c.runErr,
		Summary:    c.summary,
	}
}

func (c *swarmCoordinator) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}

func (c *swarmCoordinator) fail(err error) {
	c.mu.Lock()
	c.state = "failed"
	c.runErr = err.Error()
	c.mu.Unlock()
}

func (c *swarmCoordinator) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", c.handleHealth)
	mux.HandleFunc("GET /v1/status", c.handleStatus)
	mux.HandleFunc("POST /v1/peers/register", c.handleRegister)
	mux.HandleFunc("GET /v1/peers/config", c.handleRuntimeConfig)
	mux.HandleFunc("GET /v1/results/archive", c.handleArchive)
	return mux
}

func (c *swarmCoordinator) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeControllerJSON(w, http.StatusOK, c.status())
}

func (c *swarmCoordinator) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeControllerJSON(w, http.StatusOK, c.status())
}

func (c *swarmCoordinator) handleRegister(w http.ResponseWriter, r *http.Request) {
	var request peerkitp2p.SwarmRegisterRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&request); err != nil {
		writeControllerJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := c.register(request); err != nil {
		writeControllerJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	status := c.status()
	nodeID := c.scenario.Topology.Nodes[request.Slot-1].ID
	writeControllerJSON(w, http.StatusOK, peerkitp2p.SwarmRegisterResponse{
		NodeID: nodeID, Registered: status.Registered, Expected: status.Expected,
	})
}

func (c *swarmCoordinator) handleRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
	if err != nil || slot <= 0 {
		writeControllerJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid slot"})
		return
	}
	cfg, ok := c.runtimeConfig(slot)
	if !ok {
		status := c.status()
		writeControllerJSON(w, http.StatusTooEarly, status)
		return
	}
	writeControllerJSON(w, http.StatusOK, cfg)
}

func (c *swarmCoordinator) handleArchive(w http.ResponseWriter, _ *http.Request) {
	status := c.status()
	if status.State == "failed" {
		writeControllerJSON(w, http.StatusInternalServerError, status)
		return
	}
	if status.State != "completed" {
		writeControllerJSON(w, http.StatusTooEarly, status)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=peerkit-results.tar.gz")
	if err := writeTarGZ(w, c.resultDir); err != nil {
		return
	}
}

func writeControllerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeTarGZ(destination io.Writer, root string) error {
	gzipWriter := gzip.NewWriter(destination)
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeTarErr := tarWriter.Close()
	closeGzipErr := gzipWriter.Close()
	if walkErr != nil {
		return walkErr
	}
	if closeTarErr != nil {
		return closeTarErr
	}
	return closeGzipErr
}
