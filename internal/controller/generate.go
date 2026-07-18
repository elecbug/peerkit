package controller

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/k-p2plab/peerkit/internal/config"
)

type generatedRun struct {
	RunID            string
	Mode             string
	RunDir           string
	ResultDir        string
	ComposeFile      string
	StackFile        string
	ProjectName      string
	ControlPorts     map[string]int
	ControlEndpoints controlEndpoints
	ControllerURL    string
	MetadataFile     string
}

type identityRecord struct {
	privateKey string
	peerID     string
	service    string
	index      int
}

var projectNameSanitizer = regexp.MustCompile(`[^a-z0-9_-]+`)

func generateComposeRuntime(scenarioPath string, scenario *config.Scenario, options RunOptions) (*generatedRun, error) {
	now := time.Now().UTC()
	runID := now.Format("20060102T150405.000000000Z")
	namePart := sanitizeProjectName(scenario.Experiment.Name)
	if len(namePart) > 24 {
		namePart = namePart[:24]
	}
	projDir := fmt.Sprintf("%s", now.Format("060102"))
	projectName := fmt.Sprintf("%s-%s", namePart, now.Format("T150405"))

	runDir := options.OutputDir
	if runDir == "" {
		runDir = filepath.Join(options.ProjectRoot, ".peerkit", "runs", projDir, projectName)
	}
	absoluteRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return nil, err
	}
	resultDir := filepath.Join(absoluteRunDir, "results")
	configDir := filepath.Join(absoluteRunDir, "config")
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return nil, fmt.Errorf("create result directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	identities := make(map[string]identityRecord, len(scenario.Topology.Nodes))
	for index, node := range scenario.Topology.Nodes {
		privateKey, _, err := crypto.GenerateEd25519Key(cryptorand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate identity for %s: %w", node.ID, err)
		}
		privateKeyBytes, err := crypto.MarshalPrivateKey(privateKey)
		if err != nil {
			return nil, fmt.Errorf("marshal identity for %s: %w", node.ID, err)
		}
		peerID, err := peer.IDFromPrivateKey(privateKey)
		if err != nil {
			return nil, fmt.Errorf("derive peer id for %s: %w", node.ID, err)
		}
		identities[node.ID] = identityRecord{
			privateKey: base64.StdEncoding.EncodeToString(privateKeyBytes),
			peerID:     peerID.String(), service: fmt.Sprintf("peer%04d", index), index: index,
		}
	}

	neighbors := make(map[string][]config.RuntimeNeighborConfig, len(scenario.Topology.Nodes))
	for _, edge := range scenario.Topology.Edges {
		source := identities[edge.Source]
		target := identities[edge.Target]
		network := edge.Network.Resolve()
		neighbors[edge.Source] = append(neighbors[edge.Source], config.RuntimeNeighborConfig{
			NodeID: edge.Target, Index: target.index, PeerID: target.peerID,
			Address: fmt.Sprintf("/dns4/%s/tcp/4001", target.service), Network: network,
		})
		neighbors[edge.Target] = append(neighbors[edge.Target], config.RuntimeNeighborConfig{
			NodeID: edge.Source, Index: source.index, PeerID: source.peerID,
			Address: fmt.Sprintf("/dns4/%s/tcp/4001", source.service), Network: network,
		})
	}

	services := make(map[string]any, len(scenario.Topology.Nodes))
	controlPorts := make(map[string]int, len(scenario.Topology.Nodes))
	for index, node := range scenario.Topology.Nodes {
		identity := identities[node.ID]
		port := scenario.Experiment.ControlBasePort + index
		controlPorts[node.ID] = port
		runtimeConfig := config.RuntimeNodeConfig{
			RunID: runID, ExperimentName: scenario.Experiment.Name,
			Protocol: scenario.Protocol,
			NodeID:   node.ID, NodeIndex: index,
			Seed:          scenario.Experiment.Seed + int64(index)*1_000_003,
			PrivateKey:    identity.privateKey,
			ListenAddress: "/ip4/0.0.0.0/tcp/4001", ControlAddress: ":8080",
			ResultFile:  "/results/" + node.ID + ".jsonl",
			Metrics:     scenario.Metrics,
			Performance: *node.Performance, Neighbors: neighbors[node.ID],
		}
		configPath := filepath.Join(configDir, node.ID+".yaml")
		if err := config.WriteYAML(configPath, runtimeConfig); err != nil {
			return nil, err
		}

		service := map[string]any{
			"image":   options.Image,
			"command": []string{"-config", "/config/node.yaml"},
			"ports":   []string{fmt.Sprintf("127.0.0.1:%d:8080", port)},
			"volumes": []any{
				map[string]any{"type": "bind", "source": configPath, "target": "/config/node.yaml", "read_only": true},
				map[string]any{"type": "bind", "source": resultDir, "target": "/results"},
			},
			"networks": []string{"peerkit"},
			"restart":  "no",
		}
		if node.Resources != nil {
			if node.Resources.CPULimit > 0 {
				service["cpus"] = strconv.FormatFloat(node.Resources.CPULimit, 'f', -1, 64)
			}
			if node.Resources.MemoryLimitMB > 0 {
				service["mem_limit"] = fmt.Sprintf("%dm", node.Resources.MemoryLimitMB)
			}
		}
		services[identity.service] = service
	}

	compose := map[string]any{
		"services": services,
		"networks": map[string]any{"peerkit": map[string]any{"driver": "bridge"}},
	}
	composeFile := filepath.Join(absoluteRunDir, "compose.yaml")
	if err := config.WriteYAML(composeFile, compose); err != nil {
		return nil, err
	}

	absoluteScenario, _ := filepath.Abs(scenarioPath)
	metadata := RunMetadata{
		RunID: runID, DeploymentMode: "compose", ProjectName: projectName, ComposeFile: composeFile,
		ScenarioFile: absoluteScenario, ControlPorts: controlPorts,
	}
	metadataFile := filepath.Join(absoluteRunDir, "run.yaml")
	if err := config.WriteYAML(metadataFile, metadata); err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(scenarioPath); err == nil {
		_ = os.WriteFile(filepath.Join(absoluteRunDir, "scenario.yaml"), data, 0o644)
	}
	resolvedScenario := *scenario
	resolvedScenario.Domain = nil
	if err := config.WriteYAML(filepath.Join(absoluteRunDir, "resolved-scenario.yaml"), &resolvedScenario); err != nil {
		return nil, err
	}

	return &generatedRun{
		RunID: runID, Mode: "compose", RunDir: absoluteRunDir, ResultDir: resultDir,
		ComposeFile: composeFile, ProjectName: projectName, ControlPorts: controlPorts,
		ControlEndpoints: endpointsFromPorts(controlPorts), MetadataFile: metadataFile,
	}, nil
}

func sanitizeProjectName(value string) string {
	value = strings.ToLower(value)
	value = projectNameSanitizer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_")
	if len(value) > 55 {
		value = value[:55]
	}
	if value == "" {
		return "peerkit-run"
	}
	return value
}
