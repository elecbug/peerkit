package controller

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
)

func buildPeerImage(ctx context.Context, projectRoot, image string) error {
	return runCommand(ctx, projectRoot, os.Stdout, os.Stderr,
		"docker", "build", "-t", image, "-f", "deploy/Dockerfile", ".")
}

func composeUp(
	ctx context.Context,
	run *generatedRun,
	parallelism int,
) error {
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
