package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/k-p2plab/peerkit/internal/config"
	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

func main() {
	configPath := flag.String("config", "/config/node.yaml", "runtime node configuration path")
	bootstrapController := flag.String("bootstrap-controller", "", "Swarm controller URL used to obtain runtime configuration")
	slotFlag := flag.Int("slot", 0, "Swarm task slot; defaults to PEERKIT_TASK_SLOT")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		cfg *config.RuntimeNodeConfig
		err error
	)
	if *bootstrapController != "" {
		slot, slotErr := resolveTaskSlot(*slotFlag)
		if slotErr != nil {
			log.Fatal(slotErr)
		}
		cfg, err = peerkitp2p.BootstrapRuntime(ctx, *bootstrapController, slot)
	} else {
		cfg, err = config.LoadRuntimeNode(*configPath)
	}
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	peerNode, err := peerkitp2p.New(ctx, cfg)
	if err != nil {
		log.Fatalf("create peer: %v", err)
	}
	defer func() {
		if err := peerNode.Close(); err != nil {
			log.Printf("close peer: %v", err)
		}
	}()
	if err := peerNode.StartHTTP(); err != nil {
		log.Fatalf("start control server: %v", err)
	}

	log.Printf("peer %s started", cfg.NodeID)
	peerNode.Wait()
}

func resolveTaskSlot(explicit int) (int, error) {
	if explicit > 0 {
		return explicit, nil
	}
	value := os.Getenv("PEERKIT_TASK_SLOT")
	if value == "" {
		return 0, fmt.Errorf("swarm task slot is missing; set -slot or PEERKIT_TASK_SLOT")
	}
	slot, err := strconv.Atoi(value)
	if err != nil || slot <= 0 {
		return 0, fmt.Errorf("invalid PEERKIT_TASK_SLOT %q", value)
	}
	return slot, nil
}
