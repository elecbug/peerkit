package controller

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/metrics"
	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

type experimentExecutionOptions struct {
	ReadyTimeout time.Duration
	Download     bool
}

func executeExperiment(
	ctx context.Context,
	scenario *config.Scenario,
	endpoints controlEndpoints,
	runDir string,
	resultDir string,
	options experimentExecutionOptions,
) (*metrics.RunSummary, error) {
	client := newControlClient(scenario.Controller.Parallelism)
	readyTimeout := options.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 180 * time.Second
	}
	readyCtx, cancelReady := context.WithTimeout(ctx, readyTimeout)
	if err := client.waitReady(readyCtx, endpoints); err != nil {
		cancelReady()
		return nil, err
	}
	cancelReady()
	log.Printf("all %d peers are ready", len(endpoints))

	operationTimeout := time.Duration(scenario.Controller.OperationTimeoutSeconds) * time.Second
	connectCtx, cancelConnect := context.WithTimeout(ctx, operationTimeout)
	if err := client.connectAll(connectCtx, endpoints); err != nil {
		cancelConnect()
		return nil, err
	}
	cancelConnect()

	topologyCtx, cancelTopology := context.WithTimeout(ctx, operationTimeout)
	if err := client.waitTopology(topologyCtx, endpoints); err != nil {
		cancelTopology()
		return nil, err
	}
	cancelTopology()
	log.Printf("topology converged with %d nodes and %d edges", len(scenario.Topology.Nodes), len(scenario.Topology.Edges))

	prepareCtx, cancelPrepare := context.WithTimeout(ctx, operationTimeout)
	if err := client.prepareAll(prepareCtx, endpoints); err != nil {
		cancelPrepare()
		return nil, err
	}
	cancelPrepare()
	log.Printf("persistent propagation streams prepared")

	if !sleepContext(ctx, time.Duration(scenario.Experiment.WarmupMS)*time.Millisecond) {
		return nil, ctx.Err()
	}

	trafficSources, trafficPlan := buildTrafficPlan(scenario)
	if err := writeTrafficPlan(runDir, trafficPlan); err != nil {
		return nil, err
	}

	experimentStart := time.Now()
	var scheduleWG sync.WaitGroup
	errorChannel := make(chan error, len(trafficPlan))
	for trafficIndex, traffic := range scenario.Traffic {
		trafficIndex := trafficIndex
		traffic := traffic
		sources := trafficSources[trafficIndex]
		scheduleWG.Add(1)
		go func() {
			defer scheduleWG.Done()
			if !config.IsRandomTrafficSource(traffic.Source) {
				startDelay := time.Until(experimentStart.Add(time.Duration(traffic.StartAtMS) * time.Millisecond))
				if startDelay > 0 && !sleepContext(ctx, startDelay) {
					return
				}
				err := client.inject(ctx, endpoints[traffic.Source], peerkitp2p.InjectRequest{
					Count: traffic.Count, IntervalMS: traffic.IntervalMS,
					PayloadSizeBytes: traffic.PayloadSizeBytes,
				})
				if err != nil {
					errorChannel <- fmt.Errorf("inject from %s: %w", traffic.Source, err)
				}
				return
			}

			for messageIndex, source := range sources {
				target := experimentStart.Add(time.Duration(
					traffic.StartAtMS+int64(messageIndex)*traffic.IntervalMS,
				) * time.Millisecond)
				if delay := time.Until(target); delay > 0 && !sleepContext(ctx, delay) {
					return
				}
				err := client.inject(ctx, endpoints[source], peerkitp2p.InjectRequest{
					Count: 1, IntervalMS: 0, PayloadSizeBytes: traffic.PayloadSizeBytes,
				})
				if err != nil {
					errorChannel <- fmt.Errorf(
						"inject random traffic %d message %d from %s: %w",
						trafficIndex, messageIndex, source, err,
					)
					return
				}
			}
		}()
	}

	if !sleepContext(ctx, time.Duration(scenario.Experiment.DurationMS)*time.Millisecond) {
		return nil, ctx.Err()
	}
	scheduleWG.Wait()
	close(errorChannel)
	for scheduleErr := range errorChannel {
		if scheduleErr != nil {
			return nil, scheduleErr
		}
	}

	log.Printf("experiment duration completed; finalizing peer metrics")
	finalizeCtx, cancelFinalize := context.WithTimeout(ctx, operationTimeout)
	if err := client.finalizeAll(finalizeCtx, endpoints); err != nil {
		cancelFinalize()
		return nil, err
	}
	cancelFinalize()

	if options.Download {
		downloadCtx, cancelDownload := context.WithTimeout(ctx, operationTimeout)
		if err := client.downloadResults(downloadCtx, endpoints, resultDir); err != nil {
			cancelDownload()
			return nil, err
		}
		cancelDownload()
	}

	return metrics.Aggregate(resultDir, len(scenario.Topology.Nodes))
}
