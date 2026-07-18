package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func buildPeerImage(ctx context.Context, projectRoot, image string) error {
	return runCommand(ctx, projectRoot, os.Stdout, os.Stderr,
		"docker", "build", "-t", image, "-f", "deploy/Dockerfile", ".")
}

func pushPeerImage(ctx context.Context, projectRoot, image string) error {
	return runCommand(ctx, projectRoot, os.Stdout, os.Stderr, "docker", "push", image)
}

func ensureSwarmManager(ctx context.Context) error {
	output, err := commandOutput(ctx, ".", "docker", "info", "--format", "{{.Swarm.LocalNodeState}} {{.Swarm.ControlAvailable}}")
	if err != nil {
		return fmt.Errorf("inspect Docker Swarm state: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) != 2 || fields[0] != "active" || fields[1] != "true" {
		return fmt.Errorf("Docker Swarm manager is required; current state is %q", strings.TrimSpace(output))
	}
	return nil
}

func composeUp(ctx context.Context, run *generatedRun, parallelism int) error {
	return runCommand(
		ctx,
		run.RunDir,
		os.Stdout,
		os.Stderr,
		"docker",
		"compose",
		"--parallel",
		strconv.Itoa(parallelism),
		"-p",
		run.ProjectName,
		"-f",
		run.ComposeFile,
		"up",
		"-d",
	)
}

func composeStop(ctx context.Context, run *generatedRun) error {
	return runCommand(ctx, run.RunDir, os.Stdout, os.Stderr,
		"docker", "compose", "-p", run.ProjectName, "-f", run.ComposeFile, "stop")
}

func composeDown(ctx context.Context, projectName, composeFile, workDir string) error {
	return runCommand(ctx, workDir, os.Stdout, os.Stderr,
		"docker", "compose", "-p", projectName, "-f", composeFile, "down", "--remove-orphans")
}

func stackDeploy(
	ctx context.Context,
	run *generatedRun,
	swarm config.SwarmConfig,
	peerCount int,
	controllerPort int,
) error {
	if err := ensureSwarmPublishedPortAvailable(ctx, run.RunDir, run.ProjectName, controllerPort); err != nil {
		return err
	}

	args := []string{"stack", "deploy", "--detach", "--prune"}
	if swarm.WithRegistryAuthEnabled() {
		args = append(args, "--with-registry-auth")
	}
	args = append(args, "-c", run.StackFile, run.ProjectName)

	if err := runCommand(ctx, run.RunDir, os.Stdout, os.Stderr, "docker", args...); err != nil {
		return fmt.Errorf("deploy Swarm stack: %w", err)
	}

	controllerService := run.ProjectName + "_controller"
	peerService := run.ProjectName + "_peers"

	if err := waitServiceExists(ctx, run.RunDir, controllerService, 30*time.Second); err != nil {
		return withServiceDiagnostics(ctx, run.RunDir, controllerService, err)
	}
	if err := waitServiceExists(ctx, run.RunDir, peerService, 30*time.Second); err != nil {
		return withServiceDiagnostics(ctx, run.RunDir, peerService, err)
	}

	startupTimeout := time.Duration(swarm.StartupTimeoutSeconds) * time.Second
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	log.Printf("waiting for Swarm controller service")
	if err := waitServiceReplicas(startupCtx, run.RunDir, controllerService, 1); err != nil {
		return err
	}
	log.Printf("Swarm controller task is running")

	if err := waitControllerHTTP(startupCtx, run.ControllerURL); err != nil {
		return withServiceDiagnostics(ctx, run.RunDir, controllerService, err)
	}
	log.Printf("Swarm controller HTTP endpoint is ready: %s", run.ControllerURL)

	return scaleServiceStaged(
		startupCtx,
		run.RunDir,
		peerService,
		peerCount,
		swarm.StartupBatchSize,
		time.Duration(swarm.StartupBatchIntervalMS)*time.Millisecond,
	)
}

func ensureSwarmPublishedPortAvailable(
	ctx context.Context,
	workDir string,
	projectName string,
	publishedPort int,
) error {
	if publishedPort <= 0 {
		return nil
	}

	output, err := commandOutput(ctx, workDir, "docker", "service", "ls", "--format", "{{.Name}}")
	if err != nil {
		return fmt.Errorf("list Swarm services before deployment: %w", err)
	}

	ownController := projectName + "_controller"
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		serviceName := strings.TrimSpace(line)
		if serviceName == "" || serviceName == ownController {
			continue
		}
		ports, inspectErr := commandOutput(
			ctx,
			workDir,
			"docker",
			"service",
			"inspect",
			serviceName,
			"--format",
			"{{range .Endpoint.Ports}}{{println .PublishedPort}}{{end}}",
		)
		if inspectErr != nil {
			continue
		}
		for _, portText := range strings.Fields(ports) {
			port, parseErr := strconv.Atoi(portText)
			if parseErr == nil && port == publishedPort {
				return fmt.Errorf(
					"Swarm controller port %d is already published by service %s; remove the stale stack or choose another experiment.control_base_port",
					publishedPort,
					serviceName,
				)
			}
		}
	}
	return nil
}

func waitServiceExists(
	ctx context.Context,
	workDir string,
	serviceName string,
	timeout time.Duration,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		_, err := commandOutput(
			waitCtx,
			workDir,
			"docker",
			"service",
			"inspect",
			serviceName,
			"--format",
			"{{.ID}}",
		)
		if err == nil {
			return nil
		}
		lastErr = err

		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"Swarm service %s was not created within %s: %w; last inspect error: %v",
				serviceName,
				timeout,
				waitCtx.Err(),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitControllerHTTP(ctx context.Context, controllerURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(controllerURL, "/healthz"), nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned %s", response.Status)
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("Swarm controller HTTP endpoint %s did not become ready: %w; last error: %v", controllerURL, ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func scaleServiceStaged(
	ctx context.Context,
	workDir string,
	serviceName string,
	total int,
	batchSize int,
	batchInterval time.Duration,
) error {
	if total < 0 {
		return fmt.Errorf("peer replica count must be non-negative")
	}
	if total == 0 {
		return nil
	}
	if batchSize <= 0 {
		return fmt.Errorf("startup batch size must be positive")
	}

	for target := min(batchSize, total); ; target = min(target+batchSize, total) {
		log.Printf("requesting Swarm peer replicas: target=%d total=%d", target, total)

		if err := runCommand(
			ctx,
			workDir,
			os.Stdout,
			os.Stderr,
			"docker",
			"service",
			"scale",
			"--detach",
			fmt.Sprintf("%s=%d", serviceName, target),
		); err != nil {
			return withServiceDiagnostics(ctx, workDir, serviceName, fmt.Errorf(
				"scale Swarm service %s to %d: %w",
				serviceName,
				target,
				err,
			))
		}

		if err := waitDesiredReplicas(ctx, workDir, serviceName, target); err != nil {
			return withServiceDiagnostics(ctx, workDir, serviceName, err)
		}
		if err := waitServiceReplicas(ctx, workDir, serviceName, target); err != nil {
			return err
		}

		log.Printf("Swarm peer batch ready: %d/%d", target, total)
		if target == total {
			return nil
		}
		if !sleepContext(ctx, batchInterval) {
			return ctx.Err()
		}
	}
}

func waitDesiredReplicas(ctx context.Context, workDir, serviceName string, expected int) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, desired, err := serviceReplicaStatus(ctx, workDir, serviceName)
		if err == nil && desired == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Swarm service %s desired replicas did not become %d: %w", serviceName, expected, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitServiceReplicas(ctx context.Context, workDir, serviceName string, expected int) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	lastRunning := -1
	lastDesired := -1
	lastErrText := ""

	for {
		running, desired, err := serviceReplicaStatus(ctx, workDir, serviceName)
		if err == nil {
			if running != lastRunning || desired != lastDesired {
				log.Printf("waiting for Swarm service %s: running=%d desired=%d target=%d", serviceName, running, desired, expected)
				lastRunning = running
				lastDesired = desired
			}
			if desired == expected && running >= expected {
				return nil
			}
			if failure, failureErr := activeTaskFailure(ctx, workDir, serviceName); failureErr == nil && failure != "" {
				return withServiceDiagnostics(ctx, workDir, serviceName, fmt.Errorf("Swarm service %s has a failed active task: %s", serviceName, failure))
			}
		} else if err.Error() != lastErrText {
			log.Printf("unable to read Swarm service status for %s: %v", serviceName, err)
			lastErrText = err.Error()
		}

		select {
		case <-ctx.Done():
			return withServiceDiagnostics(ctx, workDir, serviceName, fmt.Errorf(
				"Swarm service %s did not reach %d replicas; last status=%d/%d: %w",
				serviceName,
				expected,
				lastRunning,
				lastDesired,
				ctx.Err(),
			))
		case <-ticker.C:
		}
	}
}

func serviceReplicaStatus(ctx context.Context, workDir, serviceName string) (int, int, error) {
	desiredOutput, err := commandOutput(
		ctx,
		workDir,
		"docker",
		"service",
		"inspect",
		serviceName,
		"--format",
		"{{if .Spec.Mode.Replicated}}{{.Spec.Mode.Replicated.Replicas}}{{else}}-1{{end}}",
	)
	if err != nil {
		return 0, 0, fmt.Errorf("inspect desired replicas for %s: %w", serviceName, err)
	}
	desiredText := strings.TrimSpace(desiredOutput)
	desired, err := strconv.Atoi(desiredText)
	if err != nil || desired < 0 {
		return 0, 0, fmt.Errorf("unexpected desired replica count %q for %s", desiredText, serviceName)
	}

	taskOutput, err := commandOutput(
		ctx,
		workDir,
		"docker",
		"service",
		"ps",
		serviceName,
		"--filter",
		"desired-state=running",
		"--format",
		"{{.CurrentState}}",
	)
	if err != nil {
		return 0, 0, fmt.Errorf("inspect running tasks for %s: %w", serviceName, err)
	}

	running := 0
	for _, line := range strings.Split(taskOutput, "\n") {
		state := strings.TrimSpace(line)
		if state == "Running" || strings.HasPrefix(state, "Running ") {
			running++
		}
	}
	return running, desired, nil
}

func activeTaskFailure(ctx context.Context, workDir, serviceName string) (string, error) {
	output, err := commandOutput(
		ctx,
		workDir,
		"docker",
		"service",
		"ps",
		serviceName,
		"--no-trunc",
		"--format",
		"{{.Name}}\t{{.CurrentState}}\t{{.Error}}",
	)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		state := strings.TrimSpace(fields[1])
		if strings.HasPrefix(state, "Failed ") || strings.HasPrefix(state, "Rejected ") || strings.HasPrefix(state, "Orphaned ") {
			return strings.TrimSpace(line), nil
		}
	}
	return "", nil
}

func swarmServiceDiagnostics(ctx context.Context, workDir, serviceName string) (string, error) {
	output, err := commandOutput(
		ctx,
		workDir,
		"docker",
		"service",
		"ps",
		serviceName,
		"--no-trunc",
		"--format",
		"{{.Name}}\t{{.Node}}\t{{.DesiredState}}\t{{.CurrentState}}\t{{.Error}}",
	)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func withServiceDiagnostics(ctx context.Context, workDir, serviceName string, cause error) error {
	diagnosticCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tasks, tasksErr := swarmServiceDiagnostics(diagnosticCtx, workDir, serviceName)
	logs, logsErr := commandOutput(
		diagnosticCtx,
		workDir,
		"docker",
		"service",
		"logs",
		"--raw",
		"--timestamps",
		"--tail",
		"100",
		serviceName,
	)

	var details []string
	if tasksErr == nil && strings.TrimSpace(tasks) != "" {
		details = append(details, "tasks:\n"+tasks)
	}
	if logsErr == nil && strings.TrimSpace(logs) != "" {
		details = append(details, "logs:\n"+strings.TrimSpace(logs))
	}
	if len(details) == 0 {
		return cause
	}
	return fmt.Errorf("%w\n%s", cause, strings.Join(details, "\n"))
}

func runCommand(ctx context.Context, workDir string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

func commandOutput(ctx context.Context, workDir, name string, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand(ctx, workDir, &stdout, &stderr, name, args...); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}
