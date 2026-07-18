package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func generateRuntime(scenarioPath string, scenario *config.Scenario, options RunOptions) (*generatedRun, error) {
	if scenario.Deployment.IsSwarm() {
		return generateSwarmRuntime(scenarioPath, scenario, options)
	}
	return generateComposeRuntime(scenarioPath, scenario, options)
}

func generateSwarmRuntime(scenarioPath string, scenario *config.Scenario, options RunOptions) (*generatedRun, error) {
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
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return nil, fmt.Errorf("create result directory: %w", err)
	}

	resolvedScenario := *scenario
	resolvedScenario.Domain = nil
	resolvedScenarioPath := filepath.Join(absoluteRunDir, "resolved-scenario.yaml")
	if err := config.WriteYAML(resolvedScenarioPath, &resolvedScenario); err != nil {
		return nil, err
	}
	scenarioParts, err := writeCompressedScenarioParts(resolvedScenarioPath, absoluteRunDir)
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(scenarioPath); err == nil {
		_ = os.WriteFile(filepath.Join(absoluteRunDir, "scenario.yaml"), data, 0o644)
	}

	peerPlacement := map[string]any{}
	if constraints := scenario.Deployment.Swarm.EffectivePeerConstraints(); len(constraints) > 0 {
		peerPlacement["constraints"] = constraints
	}

	peerDeploy := map[string]any{
		"mode":          "replicated",
		"replicas":      0,
		"endpoint_mode": "dnsrr",
		"restart_policy": map[string]any{
			"condition": "none",
		},
	}
	if len(peerPlacement) > 0 {
		peerDeploy["placement"] = peerPlacement
	}
	if resources := uniformPeerResources(scenario); len(resources) > 0 {
		peerDeploy["resources"] = map[string]any{"limits": resources}
	}

	stackConfigs := make(map[string]any, len(scenarioParts))
	controllerConfigs := make([]any, 0, len(scenarioParts))
	controllerPartTargets := make([]string, 0, len(scenarioParts))
	for index, path := range scenarioParts {
		name := fmt.Sprintf("scenario_part_%03d", index)
		target := fmt.Sprintf("/config/scenario.yaml.gz.part%03d", index)
		stackConfigs[name] = map[string]any{"file": path}
		controllerConfigs = append(controllerConfigs, map[string]any{
			"source": name,
			"target": target,
		})
		controllerPartTargets = append(controllerPartTargets, target)
	}

	controllerDeploy := map[string]any{
		"replicas": 1,
		"restart_policy": map[string]any{
			"condition": "none",
		},
	}
	if constraints := scenario.Deployment.Swarm.EffectiveControllerConstraints(); len(constraints) > 0 {
		controllerDeploy["placement"] = map[string]any{
			"constraints": constraints,
		}
	}

	stack := map[string]any{
		"version": "3.8",
		"services": map[string]any{
			"controller": map[string]any{
				"image":      options.Image,
				"entrypoint": []string{"/usr/local/bin/peerkit-swarm-controller"},
				"command": []string{
					"-scenario-gzip-parts", strings.Join(controllerPartTargets, ","),
					"-run-id", runID,
					"-listen", ":8080",
					"-result-dir", "/tmp/peerkit-controller",
				},
				"configs":  controllerConfigs,
				"networks": []string{"peerkit"},
				"ports": []any{
					map[string]any{
						"target":    8080,
						"published": scenario.Experiment.ControlBasePort,
						"protocol":  "tcp",
						"mode":      "ingress",
					},
				},
				"deploy": controllerDeploy,
			},
			"peers": map[string]any{
				"image": options.Image,
				"command": []string{
					"-bootstrap-controller", "http://controller:8080",
				},
				"environment": swarmPeerEnvironment(scenario),
				"networks":    []string{"peerkit"},
				"deploy":      peerDeploy,
			},
		},
		"configs": stackConfigs,
		"networks": map[string]any{
			"peerkit": swarmNetworkDefinition(scenario.Deployment.Swarm),
		},
	}
	stackFile := filepath.Join(absoluteRunDir, "stack.yaml")
	if err := config.WriteYAML(stackFile, stack); err != nil {
		return nil, err
	}

	absoluteScenario, _ := filepath.Abs(scenarioPath)
	controllerURL := fmt.Sprintf("http://127.0.0.1:%d", scenario.Experiment.ControlBasePort)
	metadata := RunMetadata{
		RunID:          runID,
		DeploymentMode: "swarm",
		ProjectName:    projectName,
		StackFile:      stackFile,
		ScenarioFile:   absoluteScenario,
		ControllerURL:  controllerURL,
	}
	metadataFile := filepath.Join(absoluteRunDir, "run.yaml")
	if err := config.WriteYAML(metadataFile, metadata); err != nil {
		return nil, err
	}

	return &generatedRun{
		RunID: runID, Mode: "swarm", RunDir: absoluteRunDir, ResultDir: resultDir,
		StackFile: stackFile, ProjectName: projectName, ControllerURL: controllerURL,
		MetadataFile: metadataFile,
	}, nil
}

func uniformPeerResources(scenario *config.Scenario) map[string]any {
	if len(scenario.Topology.Nodes) == 0 || scenario.Topology.Nodes[0].Resources == nil {
		return nil
	}
	resource := scenario.Topology.Nodes[0].Resources
	values := make(map[string]any)
	if resource.CPULimit > 0 {
		values["cpus"] = strconv.FormatFloat(resource.CPULimit, 'f', -1, 64)
	}
	if resource.MemoryLimitMB > 0 {
		values["memory"] = fmt.Sprintf("%dM", resource.MemoryLimitMB)
	}
	return values
}

func swarmPeerEnvironment(scenario *config.Scenario) map[string]any {
	values := map[string]any{
		"PEERKIT_TASK_SLOT": "{{.Task.Slot}}",
	}
	if subnet := strings.TrimSpace(scenario.Deployment.Swarm.Network.Subnet); subnet != "" {
		values["PEERKIT_OVERLAY_CIDR"] = subnet
	}
	return values
}

func swarmNetworkDefinition(swarm config.SwarmConfig) map[string]any {
	network := map[string]any{
		"driver":     "overlay",
		"attachable": swarm.NetworkAttachable(),
	}
	if subnet := strings.TrimSpace(swarm.Network.Subnet); subnet != "" {
		entry := map[string]any{"subnet": subnet}
		if gateway := strings.TrimSpace(swarm.Network.Gateway); gateway != "" {
			entry["gateway"] = gateway
		}
		network["ipam"] = map[string]any{
			"driver": "default",
			"config": []any{entry},
		}
	}
	return network
}
