package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CollectOptions struct {
	ControllerURL   string
	Timeout         time.Duration
	PollInterval    time.Duration
	RemoveTimeout   time.Duration
	KeepDeployment  bool
	RemoveOnFailure bool
}

type RunStatusReport struct {
	RunDir          string          `json:"run_dir"`
	DeploymentMode  string          `json:"deployment_mode"`
	ProjectName     string          `json:"project_name"`
	ControllerURL   string          `json:"controller_url,omitempty"`
	Controller      *SwarmRunStatus `json:"controller,omitempty"`
	ControllerError string          `json:"controller_error,omitempty"`
	RuntimeSummary  string          `json:"runtime_summary,omitempty"`
}

func LoadRunMetadata(runDir string) (RunMetadata, error) {
	metadataPath := filepath.Join(runDir, "run.yaml")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return RunMetadata{}, fmt.Errorf("read run metadata %s: %w", metadataPath, err)
	}
	var metadata RunMetadata
	if err := yamlUnmarshal(data, &metadata); err != nil {
		return RunMetadata{}, fmt.Errorf("decode run metadata: %w", err)
	}
	if metadata.ProjectName == "" {
		return RunMetadata{}, fmt.Errorf("run metadata has no project_name")
	}
	return metadata, nil
}

func InspectRun(ctx context.Context, runDir, controllerOverride string) (*RunStatusReport, error) {
	metadata, err := LoadRunMetadata(runDir)
	if err != nil {
		return nil, err
	}
	report := &RunStatusReport{
		RunDir:         runDir,
		DeploymentMode: metadata.DeploymentMode,
		ProjectName:    metadata.ProjectName,
	}
	if metadata.DeploymentMode == "swarm" || metadata.StackFile != "" {
		controllerURL := strings.TrimRight(strings.TrimSpace(controllerOverride), "/")
		if controllerURL == "" {
			controllerURL = strings.TrimRight(metadata.ControllerURL, "/")
		}
		report.ControllerURL = controllerURL
		if controllerURL != "" {
			status, statusErr := fetchSwarmStatus(ctx, controllerURL)
			if statusErr == nil {
				report.Controller = status
			} else {
				report.ControllerError = statusErr.Error()
			}
		}
		summary, summaryErr := commandOutput(ctx, runDir, "docker", "stack", "services", metadata.ProjectName, "--format", "{{.Name}} {{.Replicas}}")
		if summaryErr == nil {
			report.RuntimeSummary = strings.TrimSpace(summary)
		}
		return report, nil
	}
	if metadata.ComposeFile == "" {
		return report, nil
	}
	summary, summaryErr := commandOutput(ctx, runDir, "docker", "compose", "-p", metadata.ProjectName, "-f", metadata.ComposeFile, "ps")
	if summaryErr == nil {
		report.RuntimeSummary = strings.TrimSpace(summary)
	}
	return report, nil
}

func CollectRun(ctx context.Context, runDir string, options CollectOptions) (*SwarmRunStatus, error) {
	metadata, err := LoadRunMetadata(runDir)
	if err != nil {
		return nil, err
	}
	if metadata.DeploymentMode != "swarm" && metadata.StackFile == "" {
		return nil, fmt.Errorf("collect is only required for Swarm runs; compose results are written directly to %s", filepath.Join(runDir, "results"))
	}
	controllerURL := strings.TrimRight(strings.TrimSpace(options.ControllerURL), "/")
	if controllerURL == "" {
		controllerURL = strings.TrimRight(metadata.ControllerURL, "/")
	}
	if controllerURL == "" {
		return nil, fmt.Errorf("controller URL is missing; use --controller-url")
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Minute
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 2 * time.Second
	}
	if options.RemoveTimeout <= 0 {
		options.RemoveTimeout = 3 * time.Minute
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, options.Timeout)
	status, waitErr := waitSwarmRunInterval(waitCtx, controllerURL, options.PollInterval)
	cancelWait()
	if waitErr != nil {
		_ = SaveRunDiagnostics(context.Background(), runDir)
		if options.RemoveOnFailure && !options.KeepDeployment {
			removeCtx, cancel := context.WithTimeout(context.Background(), options.RemoveTimeout)
			_ = RemoveDeployment(removeCtx, runDir)
			cancel()
		}
		return status, waitErr
	}

	archivePath := filepath.Join(runDir, "peerkit-results.tar.gz")
	resultDir := filepath.Join(runDir, "results")
	if err := downloadAndExtractSwarmArchive(ctx, controllerURL, archivePath, resultDir); err != nil {
		_ = SaveRunDiagnostics(context.Background(), runDir)
		if options.RemoveOnFailure && !options.KeepDeployment {
			removeCtx, cancel := context.WithTimeout(context.Background(), options.RemoveTimeout)
			_ = RemoveDeployment(removeCtx, runDir)
			cancel()
		}
		return status, err
	}
	if err := verifyCollectedResults(resultDir); err != nil {
		return status, err
	}
	if !options.KeepDeployment {
		removeCtx, cancel := context.WithTimeout(context.Background(), options.RemoveTimeout)
		err = RemoveDeployment(removeCtx, runDir)
		cancel()
		if err != nil {
			return status, fmt.Errorf("results were saved, but deployment cleanup failed: %w", err)
		}
	}
	return status, nil
}

func fetchSwarmStatus(ctx context.Context, controllerURL string) (*SwarmRunStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(controllerURL, "/v1/status"), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 128*1024))
		return nil, fmt.Errorf("controller status returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var status SwarmRunStatus
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func downloadAndExtractSwarmArchive(ctx context.Context, controllerURL, archivePath, resultDir string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(controllerURL, "/v1/results/archive"), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download result archive: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 128*1024))
		return fmt.Errorf("download result archive returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return err
	}
	temporary := archivePath + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if err := validateTarGZ(temporary); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("invalid result archive: %w", err)
	}
	if err := os.Rename(temporary, archivePath); err != nil {
		return err
	}
	if err := os.RemoveAll(resultDir); err != nil {
		return err
	}
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	extractErr := extractTarGZ(archive, resultDir)
	closeErr = archive.Close()
	if extractErr != nil {
		return extractErr
	}
	return closeErr
}

func validateTarGZ(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	entries := 0
	for {
		_, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			if entries == 0 {
				return fmt.Errorf("archive is empty")
			}
			return nil
		}
		if err != nil {
			return err
		}
		entries++
	}
}

func verifyCollectedResults(resultDir string) error {
	for _, name := range []string{"summary.json", "messages.csv"} {
		info, err := os.Stat(filepath.Join(resultDir, name))
		if err != nil {
			return fmt.Errorf("required result %s is missing: %w", name, err)
		}
		if info.Size() == 0 {
			return fmt.Errorf("required result %s is empty", name)
		}
	}
	return nil
}

func RemoveDeployment(ctx context.Context, runDir string) error {
	metadata, err := LoadRunMetadata(runDir)
	if err != nil {
		return err
	}
	if metadata.DeploymentMode == "swarm" || metadata.StackFile != "" {
		return removeSwarmResources(ctx, metadata.ProjectName, runDir)
	}
	if metadata.ComposeFile == "" {
		return fmt.Errorf("compose_file is missing from run metadata")
	}
	return composeDown(ctx, metadata.ProjectName, metadata.ComposeFile, runDir)
}

func removeSwarmResources(ctx context.Context, projectName, workDir string) error {
	_ = runCommand(ctx, workDir, io.Discard, io.Discard, "docker", "stack", "rm", projectName)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		services, _ := stackResourceIDs(ctx, workDir, "service", projectName)
		networks, _ := stackResourceIDs(ctx, workDir, "network", projectName)
		configs, _ := stackResourceIDs(ctx, workDir, "config", projectName)
		if len(services) == 0 && len(networks) == 0 && len(configs) == 0 {
			return nil
		}
		if len(services) > 0 {
			args := append([]string{"service", "rm"}, services...)
			_ = runCommand(ctx, workDir, io.Discard, io.Discard, "docker", args...)
		}
		if len(networks) > 0 {
			args := append([]string{"network", "rm"}, networks...)
			_ = runCommand(ctx, workDir, io.Discard, io.Discard, "docker", args...)
		}
		if len(configs) > 0 {
			args := append([]string{"config", "rm"}, configs...)
			_ = runCommand(ctx, workDir, io.Discard, io.Discard, "docker", args...)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("remove Swarm resources for %s: %w", projectName, ctx.Err())
		case <-ticker.C:
		}
	}
}

func stackResourceIDs(ctx context.Context, workDir, resource, projectName string) ([]string, error) {
	output, err := commandOutput(ctx, workDir, "docker", resource, "ls", "--filter", "label=com.docker.stack.namespace="+projectName, "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	var values []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			values = append(values, value)
		}
	}
	return values, nil
}

func SaveRunDiagnostics(ctx context.Context, runDir string) error {
	metadata, err := LoadRunMetadata(runDir)
	if err != nil {
		return err
	}
	destination := filepath.Join(runDir, "diagnostics")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	if metadata.DeploymentMode == "swarm" || metadata.StackFile != "" {
		commands := []struct {
			name string
			args []string
		}{
			{"stack-services.txt", []string{"stack", "services", metadata.ProjectName, "--no-trunc"}},
			{"stack-tasks.txt", []string{"stack", "ps", metadata.ProjectName, "--no-trunc"}},
			{"controller.log", []string{"service", "logs", "--timestamps", "--no-trunc", metadata.ProjectName + "_controller"}},
		}
		for _, command := range commands {
			output, outputErr := commandOutput(ctx, runDir, "docker", command.args...)
			if outputErr != nil {
				output = outputErr.Error() + "\n"
			}
			_ = os.WriteFile(filepath.Join(destination, command.name), []byte(output), 0o644)
		}
		if metadata.ControllerURL != "" {
			status, statusErr := fetchSwarmStatus(ctx, metadata.ControllerURL)
			if statusErr == nil {
				data, _ := json.MarshalIndent(status, "", "  ")
				_ = os.WriteFile(filepath.Join(destination, "controller-status.json"), append(data, '\n'), 0o644)
			}
		}
		return nil
	}
	output, outputErr := commandOutput(ctx, runDir, "docker", "compose", "-p", metadata.ProjectName, "-f", metadata.ComposeFile, "ps")
	if outputErr != nil {
		output = outputErr.Error() + "\n"
	}
	return os.WriteFile(filepath.Join(destination, "compose-ps.txt"), []byte(output), 0o644)
}

func StreamRunLogs(ctx context.Context, runDir, service string, tail int, follow bool, stdout, stderr io.Writer) error {
	metadata, err := LoadRunMetadata(runDir)
	if err != nil {
		return err
	}
	if tail < 0 {
		tail = 100
	}
	if metadata.DeploymentMode == "swarm" || metadata.StackFile != "" {
		services := []string{}
		switch service {
		case "", "controller":
			services = []string{metadata.ProjectName + "_controller"}
		case "peers":
			services = []string{metadata.ProjectName + "_peers"}
		case "all":
			services = []string{metadata.ProjectName + "_controller", metadata.ProjectName + "_peers"}
		default:
			return fmt.Errorf("unknown service %q; expected controller, peers, or all", service)
		}
		runLogs := func(name string) error {
			args := []string{"service", "logs", "--timestamps", "--tail", strconv.Itoa(tail)}
			if follow {
				args = append(args, "--follow")
			}
			args = append(args, name)
			return runCommand(ctx, runDir, stdout, stderr, "docker", args...)
		}
		if !follow || len(services) == 1 {
			for _, name := range services {
				if err := runLogs(name); err != nil {
					return err
				}
			}
			return nil
		}

		var waitGroup sync.WaitGroup
		errCh := make(chan error, len(services))
		for _, name := range services {
			name := name
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				if err := runLogs(name); err != nil && !errors.Is(err, context.Canceled) {
					errCh <- err
				}
			}()
		}
		waitGroup.Wait()
		close(errCh)
		for err := range errCh {
			return err
		}
		return nil
	}
	args := []string{"compose", "-p", metadata.ProjectName, "-f", metadata.ComposeFile, "logs", "--tail", strconv.Itoa(tail)}
	if follow {
		args = append(args, "--follow")
	}
	switch service {
	case "", "all", "peers":
		// Compose has no Controller service; all services are peer containers.
	case "controller":
		return fmt.Errorf("Compose mode has no Controller container; use --service peers, all, or a peer service name")
	default:
		args = append(args, service)
	}
	return runCommand(ctx, runDir, stdout, stderr, "docker", args...)
}

func BuildImage(ctx context.Context, projectRoot, image string) error {
	return buildPeerImage(ctx, projectRoot, image)
}

func PushImage(ctx context.Context, projectRoot, image string) error {
	return pushPeerImage(ctx, projectRoot, image)
}
