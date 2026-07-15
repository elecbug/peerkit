package controller

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/metrics"
	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
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
		options.ReadyTimeoutSeconds = 60
	}

	run, err := generateRuntime(scenarioPath, scenario, options)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("run directory: %s", run.RunDir)

	if !options.NoBuild {
		log.Printf("building %s", options.Image)
		if err := buildPeerImage(ctx, options.ProjectRoot, options.Image); err != nil {
			return run, nil, err
		}
	}
	if err := composeUp(ctx, run); err != nil {
		return run, nil, err
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

	client := newControlClient()
	readyCtx, cancelReady := context.WithTimeout(ctx, time.Duration(options.ReadyTimeoutSeconds)*time.Second)
	if err := client.waitReady(readyCtx, run.ControlPorts); err != nil {
		cancelReady()
		return run, nil, err
	}
	cancelReady()
	log.Printf("all peers are ready")

	connectCtx, cancelConnect := context.WithTimeout(ctx, 30*time.Second)
	if err := client.connectAll(connectCtx, run.ControlPorts); err != nil {
		cancelConnect()
		return run, nil, err
	}
	cancelConnect()

	topologyCtx, cancelTopology := context.WithTimeout(ctx, 30*time.Second)
	if err := client.waitTopology(topologyCtx, run.ControlPorts); err != nil {
		cancelTopology()
		return run, nil, err
	}
	cancelTopology()
	log.Printf("topology converged with %d nodes and %d edges", len(scenario.Topology.Nodes), len(scenario.Topology.Edges))

	prepareCtx, cancelPrepare := context.WithTimeout(ctx, 30*time.Second)
	if err := client.prepareAll(prepareCtx, run.ControlPorts); err != nil {
		cancelPrepare()
		return run, nil, err
	}
	cancelPrepare()
	log.Printf("persistent propagation streams prepared")

	if !sleepContext(ctx, time.Duration(scenario.Experiment.WarmupMS)*time.Millisecond) {
		return run, nil, ctx.Err()
	}

	experimentStart := time.Now()
	var scheduleWG sync.WaitGroup
	errorChannel := make(chan error, len(scenario.Traffic))
	for _, traffic := range scenario.Traffic {
		traffic := traffic
		port := run.ControlPorts[traffic.Source]
		scheduleWG.Add(1)
		go func() {
			defer scheduleWG.Done()
			startDelay := time.Until(experimentStart.Add(time.Duration(traffic.StartAtMS) * time.Millisecond))
			if startDelay > 0 && !sleepContext(ctx, startDelay) {
				return
			}
			err := client.inject(ctx, port, peerkitp2p.InjectRequest{
				Count: traffic.Count, IntervalMS: traffic.IntervalMS,
				PayloadSizeBytes: traffic.PayloadSizeBytes,
			})
			if err != nil {
				errorChannel <- fmt.Errorf("inject from %s: %w", traffic.Source, err)
			}
		}()
	}

	if !sleepContext(ctx, time.Duration(scenario.Experiment.DurationMS)*time.Millisecond) {
		return run, nil, ctx.Err()
	}
	scheduleWG.Wait()
	close(errorChannel)
	for scheduleErr := range errorChannel {
		if scheduleErr != nil {
			return run, nil, scheduleErr
		}
	}
	// Stop peers before aggregation so event files are closed and no longer changing.
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
	if err := composeStop(stopCtx, run); err != nil {
		cancelStop()
		return run, nil, err
	}
	cancelStop()

	summary, err := metrics.Aggregate(run.ResultDir, len(scenario.Topology.Nodes))
	if err != nil {
		return run, nil, err
	}
	return run, summary, nil
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
