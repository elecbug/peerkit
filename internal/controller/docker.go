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

func stackDeploy(ctx context.Context, run *generatedRun, swarm config.SwarmConfig, peerCount int) error {
	args := []string{"stack", "deploy", "--prune"}
	if swarm.WithRegistryAuthEnabled() {
		args = append(args, "--with-registry-auth")
	}
	args = append(args, "-c", run.StackFile, run.ProjectName)
	if err := runCommand(ctx, run.RunDir, os.Stdout, os.Stderr, "docker", args...); err != nil {
		return err
	}

	peerService := run.ProjectName + "_peers"
	if swarm.MaxReplicasPerNode > 0 {
		if err := runCommand(
			ctx,
			run.RunDir,
			os.Stdout,
			os.Stderr,
			"docker",
			"service",
			"update",
			"--replicas-max-per-node",
			strconv.Itoa(swarm.MaxReplicasPerNode),
			peerService,
		); err != nil {
			return err
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
		log.Printf("scaling Swarm peer service to %d/%d replicas", target, total)
		if err := runCommand(
			ctx,
			workDir,
			os.Stdout,
			os.Stderr,
			"docker",
			"service",
			"scale",
			fmt.Sprintf("%s=%d", serviceName, target),
		); err != nil {
			return err
		}
		if err := waitServiceReplicas(ctx, workDir, serviceName, target); err != nil {
			return err
		}
		if target == total {
			return nil
		}
		if !sleepContext(ctx, batchInterval) {
			return ctx.Err()
		}
	}
}

func waitServiceReplicas(ctx context.Context, workDir, serviceName string, expected int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		running, desired, err := serviceReplicaStatus(ctx, workDir, serviceName)
		if err == nil && running >= expected && desired == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"Swarm service %s did not reach %d replicas (last status %d/%d): %w",
				serviceName,
				expected,
				running,
				desired,
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func serviceReplicaStatus(ctx context.Context, workDir, serviceName string) (int, int, error) {
	output, err := commandOutput(
		ctx,
		workDir,
		"docker",
		"service",
		"ls",
		"--filter",
		"name="+serviceName,
		"--format",
		"{{.Name}} {{.Replicas}}",
	)
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != serviceName {
			continue
		}
		parts := strings.SplitN(fields[1], "/", 2)
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("unexpected replica status %q", fields[1])
		}
		running, runningErr := strconv.Atoi(parts[0])
		desired, desiredErr := strconv.Atoi(parts[1])
		if runningErr != nil || desiredErr != nil {
			return 0, 0, fmt.Errorf("unexpected replica status %q", fields[1])
		}
		return running, desired, nil
	}
	return 0, 0, fmt.Errorf("Swarm service %s was not found", serviceName)
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
