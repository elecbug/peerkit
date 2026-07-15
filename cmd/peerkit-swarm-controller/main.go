package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/k-p2plab/peerkit/internal/controller"
)

func main() {
	scenarioPath := flag.String("scenario", "", "resolved experiment scenario")
	scenarioGzipParts := flag.String("scenario-gzip-parts", "", "comma-separated gzip scenario config parts")
	runID := flag.String("run-id", "swarm-run", "experiment run identifier")
	listen := flag.String("listen", ":8080", "controller HTTP listen address")
	resultDir := flag.String("result-dir", "/tmp/peerkit-controller", "controller result directory")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cleanup := func() {}
	if *scenarioGzipParts != "" {
		materialized, materializedCleanup, err := controller.MaterializeScenarioGzipParts(*scenarioGzipParts)
		if err != nil {
			log.Fatal(err)
		}
		*scenarioPath = materialized
		cleanup = materializedCleanup
	}
	defer cleanup()
	if *scenarioPath == "" {
		log.Fatal("-scenario or -scenario-gzip-parts is required")
	}
	if err := controller.ServeSwarmController(ctx, *scenarioPath, *runID, *listen, *resultDir); err != nil {
		log.Fatal(err)
	}
}
