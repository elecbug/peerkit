package controller

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/metrics"
)

func Run(ctx context.Context, scenarioPath string, options RunOptions) (*generatedRun, *metrics.RunSummary, error) {
	scenario, err := config.LoadScenario(scenarioPath)
	if err != nil {
		return nil, nil, err
	}
	if options.ProjectRoot == "" {
		options.ProjectRoot, err = os.Getwd()
		if err != nil {
			return nil, nil, err
		}
	}
	options.ProjectRoot, err = filepath.Abs(options.ProjectRoot)
	if err != nil {
		return nil, nil, err
	}
	if options.Image == "" {
		options.Image = "peerkit-peer:dev"
	}
	if options.ReadyTimeoutSeconds <= 0 {
		options.ReadyTimeoutSeconds = 180
	}

	run, err := generateRuntime(scenarioPath, scenario, options)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("run directory: %s", run.RunDir)

	if scenario.Deployment.IsSwarm() {
		summary, err := runSwarm(ctx, scenario, run, options)
		return run, summary, err
	}
	summary, err := runCompose(ctx, scenario, run, options)
	return run, summary, err
}

func runCompose(
	ctx context.Context,
	scenario *config.Scenario,
	run *generatedRun,
	options RunOptions,
) (*metrics.RunSummary, error) {
	if !scenario.Deployment.Swarm.PushImageEnabled() && !options.NoBuild {
		log.Printf("warning: push_image=false builds %s only on the manager; preload the same image on every eligible Swarm node", options.Image)
	}
	if !options.NoBuild {
		log.Printf("building %s", options.Image)
		if err := buildPeerImage(ctx, options.ProjectRoot, options.Image); err != nil {
			return nil, err
		}
	}
	if err := composeUp(ctx, run, scenario.Deployment.ComposeParallelism); err != nil {
		return nil, err
	}
	if !options.Keep {
		defer func() {
			downCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := composeDown(downCtx, run.ProjectName, run.ComposeFile, run.RunDir); err != nil {
				log.Printf("compose down: %v", err)
			}
		}()
	}

	log.Printf("waiting for %d compose peers", len(scenario.Topology.Nodes))
	summary, err := executeExperiment(
		ctx,
		scenario,
		run.ControlEndpoints,
		run.RunDir,
		run.ResultDir,
		experimentExecutionOptions{
			ReadyTimeout: time.Duration(options.ReadyTimeoutSeconds) * time.Second,
			Download:     false,
		},
	)
	if err != nil {
		return nil, err
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
	if err := composeStop(stopCtx, run); err != nil {
		cancelStop()
		return nil, err
	}
	cancelStop()
	return summary, nil
}

func runSwarm(
	ctx context.Context,
	scenario *config.Scenario,
	run *generatedRun,
	options RunOptions,
) (*metrics.RunSummary, error) {
	if err := ensureSwarmManager(ctx); err != nil {
		return nil, err
	}
	if scenario.Deployment.Swarm.PushImageEnabled() && !isPushableImageReference(options.Image) {
		return nil, fmt.Errorf("swarm image %q is not registry-qualified; use --image <registry>/<repository>:<tag> or set deployment.swarm.push_image=false after preloading the image on every node", options.Image)
	}
	if !scenario.Deployment.Swarm.PushImageEnabled() && !options.NoBuild {
		log.Printf("warning: push_image=false builds %s only on the manager; preload the same image on every eligible Swarm node", options.Image)
	}
	if !options.NoBuild {
		log.Printf("building %s", options.Image)
		if err := buildPeerImage(ctx, options.ProjectRoot, options.Image); err != nil {
			return nil, err
		}
		if scenario.Deployment.Swarm.PushImageEnabled() {
			log.Printf("pushing %s for Swarm workers", options.Image)
			if err := pushPeerImage(ctx, options.ProjectRoot, options.Image); err != nil {
				return nil, fmt.Errorf("push swarm image: %w", err)
			}
		}
	}

	log.Printf("deploying Swarm stack %s with %d peer tasks", run.ProjectName, len(scenario.Topology.Nodes))
	if err := stackDeploy(ctx, run, scenario.Deployment.Swarm, len(scenario.Topology.Nodes)); err != nil {
		return nil, err
	}
	if !options.Keep {
		defer func() {
			downCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := stackRemove(downCtx, run.ProjectName, run.RunDir); err != nil {
				log.Printf("stack remove: %v", err)
			}
		}()
	}

	totalTimeout := time.Duration(scenario.Deployment.Swarm.StartupTimeoutSeconds)*time.Second +
		time.Duration(scenario.Controller.OperationTimeoutSeconds*4)*time.Second +
		time.Duration(scenario.Experiment.WarmupMS+scenario.Experiment.DurationMS)*time.Millisecond +
		5*time.Minute
	waitCtx, cancelWait := context.WithTimeout(ctx, totalTimeout)
	status, err := waitSwarmRun(waitCtx, run.ControllerURL)
	cancelWait()
	if err != nil {
		return nil, err
	}
	if status.Summary == nil {
		return nil, fmt.Errorf("swarm controller completed without a summary")
	}

	downloadCtx, cancelDownload := context.WithTimeout(ctx, 10*time.Minute)
	if err := downloadSwarmArchive(downloadCtx, run.ControllerURL, run.ResultDir); err != nil {
		cancelDownload()
		return nil, err
	}
	cancelDownload()
	return status.Summary, nil
}

func Down(ctx context.Context, runDir string) error {
	metadataPath := filepath.Join(runDir, "run.yaml")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("read run metadata: %w", err)
	}
	var metadata RunMetadata
	if err := yamlUnmarshal(data, &metadata); err != nil {
		return err
	}
	if metadata.DeploymentMode == "swarm" || metadata.StackFile != "" {
		return stackRemove(ctx, metadata.ProjectName, runDir)
	}
	return composeDown(ctx, metadata.ProjectName, metadata.ComposeFile, runDir)
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isPushableImageReference(image string) bool {
	for index, r := range image {
		if r == '/' && index > 0 {
			return true
		}
	}
	return false
}
