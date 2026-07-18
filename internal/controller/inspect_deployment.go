package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type InspectionSeverity string

const (
	InspectionOK      InspectionSeverity = "ok"
	InspectionWarning InspectionSeverity = "warning"
	InspectionError   InspectionSeverity = "error"
)

type InspectionFinding struct {
	Severity InspectionSeverity `json:"severity"`
	Category string             `json:"category"`
	Summary  string             `json:"summary"`
	Details  map[string]any     `json:"details,omitempty"`
	Advice   []string           `json:"advice,omitempty"`
}

type DeploymentInspectionReport struct {
	CheckedAt      time.Time           `json:"checked_at"`
	RunDir         string              `json:"run_dir"`
	DeploymentMode string              `json:"deployment_mode"`
	ProjectName    string              `json:"project_name"`
	Overall        InspectionSeverity  `json:"overall"`
	Findings       []InspectionFinding `json:"findings"`
}

type swarmTaskInspection struct {
	Name         string `json:"name"`
	Node         string `json:"node,omitempty"`
	DesiredState string `json:"desired_state"`
	CurrentState string `json:"current_state"`
	Error        string `json:"error,omitempty"`
}

type dockerNetworkInspection struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Driver string `json:"Driver"`
	Scope  string `json:"Scope"`
	IPAM   struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
}

func InspectDeployment(ctx context.Context, runDir string) (*DeploymentInspectionReport, error) {
	absoluteRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return nil, err
	}
	metadata, err := LoadRunMetadata(absoluteRunDir)
	if err != nil {
		return nil, err
	}
	report := &DeploymentInspectionReport{
		CheckedAt:      time.Now().UTC(),
		RunDir:         absoluteRunDir,
		DeploymentMode: metadata.DeploymentMode,
		ProjectName:    metadata.ProjectName,
		Overall:        InspectionOK,
	}
	add := func(finding InspectionFinding) {
		report.Findings = append(report.Findings, finding)
		report.Overall = maxInspectionSeverity(report.Overall, finding.Severity)
	}

	if metadata.DeploymentMode != "swarm" && metadata.StackFile == "" {
		inspectComposeDeployment(ctx, absoluteRunDir, metadata, add)
		return report, nil
	}

	inspectSwarmControlPlane(ctx, absoluteRunDir, add)
	services, _ := stackResourceIDs(ctx, absoluteRunDir, "service", metadata.ProjectName)
	networks, _ := stackResourceIDs(ctx, absoluteRunDir, "network", metadata.ProjectName)
	configs, _ := stackResourceIDs(ctx, absoluteRunDir, "config", metadata.ProjectName)
	if len(services) == 0 && len(networks) == 0 && len(configs) == 0 {
		if err := verifyCollectedResults(filepath.Join(absoluteRunDir, "results")); err == nil {
			add(InspectionFinding{
				Severity: InspectionOK,
				Category: "lifecycle",
				Summary:  "The experiment results are present and all deployment resources have been removed",
			})
			return report, nil
		}
		add(InspectionFinding{
			Severity: InspectionError,
			Category: "lifecycle",
			Summary:  "No stack resources are present and complete results were not found",
			Advice:   []string{"Check whether the run was stopped before result collection completed."},
		})
		return report, nil
	}
	controllerDesired := inspectSwarmService(ctx, absoluteRunDir, metadata.ProjectName+"_controller", "controller", add)
	peerDesired := inspectSwarmService(ctx, absoluteRunDir, metadata.ProjectName+"_peers", "peers", add)
	inspectSwarmNetworks(ctx, absoluteRunDir, metadata.ProjectName, controllerDesired+peerDesired, add)
	inspectSwarmConfigs(ctx, absoluteRunDir, metadata.ProjectName, add)
	inspectControllerEndpoint(ctx, absoluteRunDir, metadata, add)
	inspectPublishedPortConflicts(ctx, absoluteRunDir, metadata, add)

	sort.SliceStable(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity != report.Findings[j].Severity {
			return inspectionSeverityRank(report.Findings[i].Severity) > inspectionSeverityRank(report.Findings[j].Severity)
		}
		return report.Findings[i].Category < report.Findings[j].Category
	})
	return report, nil
}

func inspectComposeDeployment(ctx context.Context, runDir string, metadata RunMetadata, add func(InspectionFinding)) {
	if metadata.ComposeFile == "" {
		add(InspectionFinding{Severity: InspectionError, Category: "compose", Summary: "compose_file is missing from run metadata"})
		return
	}
	output, err := commandOutput(ctx, runDir, "docker", "compose", "-p", metadata.ProjectName, "-f", metadata.ComposeFile, "ps")
	if err != nil {
		add(InspectionFinding{Severity: InspectionError, Category: "compose", Summary: "Docker Compose status could not be read", Details: map[string]any{"error": err.Error()}})
		return
	}
	trimmed := strings.TrimSpace(output)
	severity := InspectionOK
	summary := "Compose deployment is visible"
	if trimmed == "" {
		severity = InspectionWarning
		summary = "Compose deployment has no visible containers"
	}
	add(InspectionFinding{Severity: severity, Category: "compose", Summary: summary, Details: map[string]any{"ps": trimmed}})
}

func inspectSwarmControlPlane(ctx context.Context, workDir string, add func(InspectionFinding)) {
	info, err := commandOutput(ctx, workDir, "docker", "info", "--format", "{{.Swarm.LocalNodeState}}\t{{.Swarm.ControlAvailable}}\t{{.Swarm.NodeID}}\t{{.Swarm.Nodes}}\t{{.Swarm.Managers}}")
	if err != nil {
		add(InspectionFinding{Severity: InspectionError, Category: "swarm", Summary: "Docker Swarm state could not be read", Details: map[string]any{"error": err.Error()}})
		return
	}
	fields := strings.Split(strings.TrimSpace(info), "\t")
	if len(fields) < 2 || fields[0] != "active" || fields[1] != "true" {
		add(InspectionFinding{Severity: InspectionError, Category: "swarm", Summary: "The current Docker daemon is not an active Swarm manager", Details: map[string]any{"docker_info": strings.TrimSpace(info)}, Advice: []string{"Run this command on an active Swarm manager."}})
		return
	}

	nodeOutput, nodeErr := commandOutput(ctx, workDir, "docker", "node", "ls", "--format", "{{.Hostname}}\t{{.Status}}\t{{.Availability}}\t{{.ManagerStatus}}")
	if nodeErr != nil {
		add(InspectionFinding{Severity: InspectionWarning, Category: "nodes", Summary: "Swarm nodes could not be listed", Details: map[string]any{"error": nodeErr.Error()}})
		return
	}
	readyActive := 0
	leader := 0
	var unhealthy []string
	for _, line := range nonEmptyLines(nodeOutput) {
		parts := strings.Split(line, "\t")
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		if parts[1] == "Ready" && parts[2] == "Active" {
			readyActive++
		} else {
			unhealthy = append(unhealthy, line)
		}
		if strings.Contains(parts[3], "Leader") {
			leader++
		}
	}
	severity := InspectionOK
	summary := fmt.Sprintf("Swarm control plane is active with %d Ready/Active node(s)", readyActive)
	var advice []string
	if readyActive == 0 || leader == 0 {
		severity = InspectionError
		summary = "Swarm has no usable scheduling node or no manager leader"
		advice = append(advice, "Ensure at least one node is Ready and Active and that one manager is Leader.")
	} else if len(unhealthy) > 0 {
		severity = InspectionWarning
		summary = fmt.Sprintf("Swarm is active, but %d node(s) are not Ready/Active", len(unhealthy))
	}
	add(InspectionFinding{Severity: severity, Category: "nodes", Summary: summary, Details: map[string]any{"ready_active": readyActive, "leaders": leader, "unhealthy": unhealthy}, Advice: advice})
}

func inspectSwarmService(ctx context.Context, workDir, serviceName, category string, add func(InspectionFinding)) int {
	if _, err := commandOutput(ctx, workDir, "docker", "service", "inspect", serviceName, "--format", "{{.ID}}"); err != nil {
		add(InspectionFinding{Severity: InspectionError, Category: category, Summary: fmt.Sprintf("Swarm service %s does not exist", serviceName), Details: map[string]any{"error": err.Error()}})
		return 0
	}

	running, desired, replicaErr := serviceReplicaStatus(ctx, workDir, serviceName)
	tasks, taskErr := inspectServiceTasks(ctx, workDir, serviceName)
	image, _ := commandOutput(ctx, workDir, "docker", "service", "inspect", serviceName, "--format", "{{.Spec.TaskTemplate.ContainerSpec.Image}}")

	states := map[string]int{}
	unassignedNew := 0
	failed := 0
	var failures []swarmTaskInspection
	for _, task := range tasks {
		state := firstStateWord(task.CurrentState)
		states[state]++
		if state == "New" && task.Node == "" {
			unassignedNew++
		}
		if state == "Failed" || state == "Rejected" || state == "Orphaned" {
			failed++
			failures = append(failures, task)
		}
	}

	severity := InspectionOK
	summary := fmt.Sprintf("%s replicas are healthy: %d/%d running", category, running, desired)
	var advice []string
	if replicaErr != nil || taskErr != nil {
		severity = InspectionError
		summary = fmt.Sprintf("%s service status could not be fully inspected", category)
	} else if failed > 0 {
		severity = InspectionError
		summary = fmt.Sprintf("%s has %d failed or rejected task(s)", category, failed)
		advice = append(advice, "Inspect the task errors and Docker daemon logs before retrying the run.")
	} else if unassignedNew > 0 {
		severity = InspectionError
		summary = fmt.Sprintf("%s has %d New task(s) without an assigned node", category, unassignedNew)
		advice = append(advice, "Check node availability, placement constraints, manager leadership, and overlay network allocation.")
	} else if running < desired {
		severity = InspectionWarning
		summary = fmt.Sprintf("%s has not converged: %d/%d running", category, running, desired)
		advice = append(advice, "Check Pending and Preparing task states and wait for convergence or inspect daemon logs.")
	}

	details := map[string]any{
		"service":    serviceName,
		"running":    running,
		"desired":    desired,
		"states":     states,
		"image":      strings.TrimSpace(image),
		"task_count": len(tasks),
	}
	if replicaErr != nil {
		details["replica_error"] = replicaErr.Error()
	}
	if taskErr != nil {
		details["task_error"] = taskErr.Error()
	}
	if len(failures) > 0 {
		details["failures"] = failures
	}
	add(InspectionFinding{Severity: severity, Category: category, Summary: summary, Details: details, Advice: advice})
	return desired
}

func inspectServiceTasks(ctx context.Context, workDir, serviceName string) ([]swarmTaskInspection, error) {
	output, err := commandOutput(ctx, workDir, "docker", "service", "ps", serviceName, "--no-trunc", "--format", "{{.Name}}\t{{.Node}}\t{{.DesiredState}}\t{{.CurrentState}}\t{{.Error}}")
	if err != nil {
		return nil, err
	}
	var tasks []swarmTaskInspection
	for _, line := range nonEmptyLines(output) {
		parts := strings.SplitN(line, "\t", 5)
		for len(parts) < 5 {
			parts = append(parts, "")
		}
		tasks = append(tasks, swarmTaskInspection{Name: parts[0], Node: parts[1], DesiredState: parts[2], CurrentState: parts[3], Error: parts[4]})
	}
	return tasks, nil
}

func inspectSwarmNetworks(ctx context.Context, workDir, projectName string, required int, add func(InspectionFinding)) {
	ids, err := stackResourceIDs(ctx, workDir, "network", projectName)
	if err != nil {
		add(InspectionFinding{Severity: InspectionError, Category: "network", Summary: "Stack networks could not be listed", Details: map[string]any{"error": err.Error()}})
		return
	}
	if len(ids) == 0 {
		add(InspectionFinding{Severity: InspectionError, Category: "network", Summary: "The stack has no overlay network"})
		return
	}

	allNetworks, _ := listDockerNetworks(ctx, workDir)
	for _, id := range ids {
		networks, inspectErr := inspectDockerNetworks(ctx, workDir, []string{id})
		if inspectErr != nil || len(networks) == 0 {
			add(InspectionFinding{Severity: InspectionError, Category: "network", Summary: fmt.Sprintf("Network %s could not be inspected", id), Details: map[string]any{"error": errorText(inspectErr)}})
			continue
		}
		network := networks[0]
		severity := InspectionOK
		summary := fmt.Sprintf("Overlay network %s is available", network.Name)
		details := map[string]any{"name": network.Name, "driver": network.Driver, "scope": network.Scope, "required_endpoints": required}
		var advice []string
		if network.Driver != "overlay" || network.Scope != "swarm" {
			severity = InspectionError
			summary = fmt.Sprintf("Network %s is not a Swarm overlay network", network.Name)
		}
		var subnets []map[string]any
		for _, cfg := range network.IPAM.Config {
			entry := map[string]any{"subnet": cfg.Subnet, "gateway": cfg.Gateway}
			if capacity, capacityErr := ipv4CIDRCapacity(cfg.Subnet); capacityErr == nil {
				entry["address_capacity"] = capacity
				if required > 0 && capacity < int64(required+8) {
					severity = InspectionError
					summary = fmt.Sprintf("Overlay subnet %s is too small for approximately %d endpoints", cfg.Subnet, required)
					advice = append(advice, "Use a larger, non-overlapping subnet and recreate the stack network.")
				} else if required > 0 && capacity < int64(float64(required+8)*1.25) && severity != InspectionError {
					severity = InspectionWarning
					summary = fmt.Sprintf("Overlay subnet %s has little spare address capacity", cfg.Subnet)
				}
			}
			overlaps := overlappingNetworkNames(cfg.Subnet, network.ID, allNetworks)
			if len(overlaps) > 0 {
				entry["overlaps"] = overlaps
				severity = InspectionError
				summary = fmt.Sprintf("Overlay subnet %s overlaps another Docker network", cfg.Subnet)
				advice = append(advice, "Choose a subnet that does not overlap host routes or existing Docker networks.")
			}
			subnets = append(subnets, entry)
		}
		details["ipam"] = subnets
		add(InspectionFinding{Severity: severity, Category: "network", Summary: summary, Details: details, Advice: uniqueStrings(advice)})
	}
}

func inspectSwarmConfigs(ctx context.Context, workDir, projectName string, add func(InspectionFinding)) {
	ids, err := stackResourceIDs(ctx, workDir, "config", projectName)
	if err != nil {
		add(InspectionFinding{Severity: InspectionWarning, Category: "configs", Summary: "Stack configs could not be listed", Details: map[string]any{"error": err.Error()}})
		return
	}
	if len(ids) == 0 {
		add(InspectionFinding{Severity: InspectionError, Category: "configs", Summary: "No scenario config parts remain for the stack"})
		return
	}
	add(InspectionFinding{Severity: InspectionOK, Category: "configs", Summary: fmt.Sprintf("%d scenario config part(s) are present", len(ids)), Details: map[string]any{"config_ids": ids}})
}

func inspectControllerEndpoint(ctx context.Context, runDir string, metadata RunMetadata, add func(InspectionFinding)) {
	if strings.TrimSpace(metadata.ControllerURL) == "" {
		add(InspectionFinding{Severity: InspectionWarning, Category: "controller-http", Summary: "Controller URL is missing from run metadata"})
		return
	}
	report, err := InspectRun(ctx, runDir, "")
	if err != nil {
		add(InspectionFinding{Severity: InspectionWarning, Category: "controller-http", Summary: "Controller HTTP status could not be inspected", Details: map[string]any{"url": metadata.ControllerURL, "error": err.Error()}})
		return
	}
	if report.ControllerError != "" {
		add(InspectionFinding{Severity: InspectionWarning, Category: "controller-http", Summary: "Controller HTTP endpoint is not responding", Details: map[string]any{"url": metadata.ControllerURL, "error": report.ControllerError}})
		return
	}
	if report.Controller == nil {
		add(InspectionFinding{Severity: InspectionWarning, Category: "controller-http", Summary: "Controller returned no run status", Details: map[string]any{"url": metadata.ControllerURL}})
		return
	}
	severity := InspectionOK
	if report.Controller.State == "failed" {
		severity = InspectionError
	}
	add(InspectionFinding{Severity: severity, Category: "controller-http", Summary: fmt.Sprintf("Controller run state is %s with %d/%d peers", report.Controller.State, report.Controller.Registered, report.Controller.Expected), Details: map[string]any{"url": metadata.ControllerURL, "state": report.Controller.State, "registered": report.Controller.Registered, "expected": report.Controller.Expected, "error": report.Controller.Error}})
}

func inspectPublishedPortConflicts(ctx context.Context, workDir string, metadata RunMetadata, add func(InspectionFinding)) {
	parsed, err := url.Parse(metadata.ControllerURL)
	if err != nil || parsed.Port() == "" {
		return
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return
	}
	serviceOutput, err := commandOutput(ctx, workDir, "docker", "service", "ls", "--format", "{{.Name}}")
	if err != nil {
		return
	}
	own := metadata.ProjectName + "_controller"
	var conflicts []string
	for _, service := range nonEmptyLines(serviceOutput) {
		if service == own {
			continue
		}
		ports, inspectErr := commandOutput(ctx, workDir, "docker", "service", "inspect", service, "--format", "{{range .Endpoint.Ports}}{{println .PublishedPort}}{{end}}")
		if inspectErr != nil {
			continue
		}
		for _, value := range strings.Fields(ports) {
			if value == strconv.Itoa(port) {
				conflicts = append(conflicts, service)
			}
		}
	}
	if len(conflicts) > 0 {
		add(InspectionFinding{Severity: InspectionError, Category: "ports", Summary: fmt.Sprintf("Controller port %d is also published by another service", port), Details: map[string]any{"conflicting_services": conflicts}, Advice: []string{"Remove the stale service or use a different experiment.control_base_port."}})
	} else {
		add(InspectionFinding{Severity: InspectionOK, Category: "ports", Summary: fmt.Sprintf("Controller port %d has no conflicting Swarm service", port)})
	}
}

func listDockerNetworks(ctx context.Context, workDir string) ([]dockerNetworkInspection, error) {
	output, err := commandOutput(ctx, workDir, "docker", "network", "ls", "-q")
	if err != nil {
		return nil, err
	}
	return inspectDockerNetworks(ctx, workDir, nonEmptyLines(output))
}

func inspectDockerNetworks(ctx context.Context, workDir string, ids []string) ([]dockerNetworkInspection, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"network", "inspect"}, ids...)
	output, err := commandOutput(ctx, workDir, "docker", args...)
	if err != nil {
		return nil, err
	}
	var networks []dockerNetworkInspection
	if err := json.Unmarshal([]byte(output), &networks); err != nil {
		return nil, err
	}
	return networks, nil
}

func overlappingNetworkNames(subnet, ownID string, networks []dockerNetworkInspection) []string {
	_, target, err := net.ParseCIDR(strings.TrimSpace(subnet))
	if err != nil {
		return nil
	}
	var overlaps []string
	for _, network := range networks {
		if network.ID == ownID {
			continue
		}
		for _, cfg := range network.IPAM.Config {
			_, candidate, candidateErr := net.ParseCIDR(strings.TrimSpace(cfg.Subnet))
			if candidateErr != nil {
				continue
			}
			if target.Contains(candidate.IP) || candidate.Contains(target.IP) {
				overlaps = append(overlaps, network.Name+" ("+cfg.Subnet+")")
			}
		}
	}
	return uniqueStrings(overlaps)
}

func ipv4CIDRCapacity(cidr string) (int64, error) {
	ip, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return 0, err
	}
	if ip.To4() == nil {
		return 0, fmt.Errorf("not IPv4")
	}
	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	if hostBits < 0 || hostBits > 30 {
		return 0, fmt.Errorf("unsupported IPv4 prefix")
	}
	return int64(1) << hostBits, nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func firstStateWord(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "Unknown"
	}
	return fields[0]
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		if _, exists := seen[value]; exists || value == "" {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func inspectionSeverityRank(severity InspectionSeverity) int {
	switch severity {
	case InspectionError:
		return 2
	case InspectionWarning:
		return 1
	default:
		return 0
	}
}

func maxInspectionSeverity(left, right InspectionSeverity) InspectionSeverity {
	if inspectionSeverityRank(right) > inspectionSeverityRank(left) {
		return right
	}
	return left
}
