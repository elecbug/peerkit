package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
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
) error {
	args := []string{"stack", "deploy", "--prune"}

	if swarm.WithRegistryAuthEnabled() {
		args = append(args, "--with-registry-auth")
	}

	args = append(args, "-c", run.StackFile, run.ProjectName)

	if err := runCommand(
		ctx,
		run.RunDir,
		os.Stdout,
		os.Stderr,
		"docker",
		args...,
	); err != nil {
		return err
	}

	peerService := run.ProjectName + "_peers"

	if err := waitServiceExists(
		ctx,
		run.RunDir,
		peerService,
		30*time.Second,
	); err != nil {
		return err
	}

	if swarm.MaxReplicasPerNode > 0 {
		if err := runCommand(
			ctx,
			run.RunDir,
			os.Stdout,
			os.Stderr,
			"docker",
			"service",
			"update",
			"--detach",
			"--replicas-max-per-node",
			strconv.Itoa(swarm.MaxReplicasPerNode),
			peerService,
		); err != nil {
			return fmt.Errorf(
				"set max replicas per node for %s: %w",
				peerService,
				err,
			)
		}
	}

	startupCtx, cancel := context.WithTimeout(
		ctx,
		time.Duration(swarm.StartupTimeoutSeconds)*time.Second,
	)
	defer cancel()

	return scaleServiceStaged(
		startupCtx,
		run.RunDir,
		peerService,
		peerCount,
		swarm.StartupBatchSize,
		time.Duration(swarm.StartupBatchIntervalMS)*time.Millisecond,
	)
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

		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"Swarm service %s was not created within %s: %w",
				serviceName,
				timeout,
				waitCtx.Err(),
			)

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

	for target := min(batchSize, total); ; {
		log.Printf(
			"scaling Swarm peer service: target=%d total=%d",
			target,
			total,
		)

		if err := runCommand(
			ctx,
			workDir,
			os.Stdout,
			os.Stderr,
			"docker",
			"service",
			"update",
			"--detach",
			"--replicas",
			strconv.Itoa(target),
			serviceName,
		); err != nil {
			return fmt.Errorf(
				"scale Swarm service %s to %d: %w",
				serviceName,
				target,
				err,
			)
		}

		if err := waitServiceReplicas(
			ctx,
			workDir,
			serviceName,
			target,
		); err != nil {
			return err
		}

		log.Printf(
			"Swarm peer batch ready: %d/%d",
			target,
			total,
		)

		if target == total {
			return nil
		}

		if !sleepContext(ctx, batchInterval) {
			return ctx.Err()
		}

		target = min(target+batchSize, total)
	}
}

func waitServiceReplicas(
	ctx context.Context,
	workDir string,
	serviceName string,
	expected int,
) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastRunning := -1
	lastDesired := -1
	lastErrText := ""

	for {
		running, desired, err := serviceReplicaStatus(
			ctx,
			workDir,
			serviceName,
		)

		if err == nil {
			if running != lastRunning || desired != lastDesired {
				log.Printf(
					"waiting for Swarm peer tasks: running=%d desired=%d target=%d",
					running,
					desired,
					expected,
				)

				lastRunning = running
				lastDesired = desired
			}

			if desired == expected && running >= expected {
				return nil
			}
		} else if err.Error() != lastErrText {
			log.Printf(
				"unable to read Swarm service status: %v",
				err,
			)
			lastErrText = err.Error()
		}

		select {
		case <-ctx.Done():
			diagnostics, diagnosticsErr := swarmServiceDiagnostics(
				context.Background(),
				workDir,
				serviceName,
			)
			if diagnosticsErr != nil {
				diagnostics = fmt.Sprintf(
					"failed to collect task diagnostics: %v",
					diagnosticsErr,
				)
			}

			return fmt.Errorf(
				"Swarm service %s did not reach %d replicas; "+
					"last status=%d/%d: %w\n%s",
				serviceName,
				expected,
				lastRunning,
				lastDesired,
				ctx.Err(),
				diagnostics,
			)

		case <-ticker.C:
		}
	}
}

func swarmServiceDiagnostics(
	ctx context.Context,
	workDir string,
	serviceName string,
) (string, error) {
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

func serviceReplicaStatus(
	ctx context.Context,
	workDir string,
	serviceName string,
) (int, int, error) {
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
		return 0, 0, fmt.Errorf(
			"inspect desired replicas for %s: %w",
			serviceName,
			err,
		)
	}

	desiredText := strings.TrimSpace(desiredOutput)

	desired, err := strconv.Atoi(desiredText)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"parse desired replica count %q for %s: %w",
			desiredText,
			serviceName,
			err,
		)
	}

	if desired < 0 {
		return 0, 0, fmt.Errorf(
			"Swarm service %s is not a replicated service",
			serviceName,
		)
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
		return 0, 0, fmt.Errorf(
			"inspect running tasks for %s: %w",
			serviceName,
			err,
		)
	}

	running := 0

	for _, line := range strings.Split(taskOutput, "\n") {
		state := strings.TrimSpace(line)

		if strings.HasPrefix(state, "Running ") ||
			state == "Running" {
			running++
		}
	}

	return running, desired, nil
}

func stackRemove(ctx context.Context, projectName, workDir string) error {
	return runCommand(ctx, workDir, os.Stdout, os.Stderr, "docker", "stack", "rm", projectName)
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
