package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/k-p2plab/peerkit/internal/config"
	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

func main() {
	configPath := flag.String("config", "/config/node.yaml", "runtime node configuration path")
	flag.Parse()

	cfg, err := config.LoadRuntimeNode(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
