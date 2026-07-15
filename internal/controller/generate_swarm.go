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
	projectName := fmt.Sprintf("peerkit-%s-%s", namePart, now.Format("060102T150405Z"))

	runDir := options.OutputDir
	if runDir == "" {
		runDir = filepath.Join(options.ProjectRoot, ".peerkit", "runs", projectName)
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

	placement := map[string]any{}
	if len(scenario.Deployment.Swarm.PlacementConstraints) > 0 {
		placement["constraints"] = scenario.Deployment.Swarm.PlacementConstraints
	}

	peerDeploy := map[string]any{
		"mode":          "replicated",
		"replicas":      0,
		"endpoint_mode": "dnsrr",
		"restart_policy": map[string]any{
			"condition": "none",
		},
	}
	if len(placement) > 0 {
		peerDeploy["placement"] = placement
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
	if len(scenario.Deployment.Swarm.PlacementConstraints) > 0 {
		controllerDeploy["placement"] = map[string]any{
			"constraints": scenario.Deployment.Swarm.PlacementConstraints,
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
				"environment": map[string]any{
					"PEERKIT_TASK_SLOT": "{{.Task.Slot}}",
				},
				"networks": []string{"peerkit"},
				"deploy":   peerDeploy,
			},
		},
		"configs": stackConfigs,
		"networks": map[string]any{
			"peerkit": map[string]any{
				"driver":     "overlay",
				"attachable": true,
				"ipam": map[string]any{
					"driver": "default",
					"config": []any{
						map[string]any{
							"subnet": "10.200.0.0/16",
						},
					},
				},
			},
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
