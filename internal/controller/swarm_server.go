package controller

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func ServeSwarmController(
	ctx context.Context,
	scenarioPath string,
	runID string,
	listenAddress string,
	resultDir string,
) error {
	scenario, err := config.LoadScenario(scenarioPath)
	if err != nil {
		return err
	}
	if !scenario.Deployment.IsSwarm() {
		return fmt.Errorf("swarm controller requires deployment.mode=swarm")
	}
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return err
	}
	if data, err := os.ReadFile(scenarioPath); err == nil {
		_ = os.WriteFile(filepath.Join(resultDir, "scenario.yaml"), data, 0o644)
	}
	resolved := *scenario
	resolved.Domain = nil
	if err := config.WriteYAML(filepath.Join(resultDir, "resolved-scenario.yaml"), &resolved); err != nil {
		return err
	}

	coordinator, err := newSwarmCoordinator(ctx, scenario, runID, resultDir)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           coordinator.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("swarm controller listening on %s", listenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()
	coordinator.start()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErr:
		return err
	}
}
