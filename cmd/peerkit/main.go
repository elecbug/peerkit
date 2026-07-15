package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/k-p2plab/peerkit/internal/config"
	"github.com/k-p2plab/peerkit/internal/controller"
)

const version = "0.2.2"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "validate":
		validateCommand(os.Args[2:])
	case "run":
		runCommand(os.Args[2:])
	case "expand":
		expandCommand(os.Args[2:])
	case "down":
		downCommand(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func validateCommand(args []string) {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	flags.Parse(args)
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: peerkit validate <scenario.yaml>")
		os.Exit(2)
	}
	scenario, err := config.LoadScenario(flags.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("valid: %s (%d nodes, %d edges, %d traffic patterns)\n",
		scenario.Experiment.Name, len(scenario.Topology.Nodes),
		len(scenario.Topology.Edges), len(scenario.Traffic))
}

func expandCommand(args []string) {
	flags := flag.NewFlagSet("expand", flag.ExitOnError)
	output := flags.String("output", "", "write the resolved explicit scenario to this file; defaults to stdout")
	flags.StringVar(output, "o", "", "alias for -output")
	flags.Parse(args)
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: peerkit expand [-o resolved.yaml] <scenario.yaml>")
		os.Exit(2)
	}

	scenario, err := config.LoadScenario(flags.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	resolved := *scenario
	resolved.Domain = nil
	data, err := yaml.Marshal(&resolved)
	if err != nil {
		log.Fatal(err)
	}
	if *output == "" {
		_, _ = os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("resolved scenario: %s (%d nodes, %d edges)\n", *output,
		len(resolved.Topology.Nodes), len(resolved.Topology.Edges))
}

func runCommand(args []string) {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	projectRoot := flags.String("project-root", ".", "peerkit source tree used as the Docker build context")
	output := flags.String("output", "", "run output directory")
	image := flags.String("image", "peerkit-peer:dev", "peer image tag")
	noBuild := flags.Bool("no-build", false, "skip docker image build")
	keep := flags.Bool("keep", false, "keep containers running after the experiment")
	readyTimeout := flags.Int("ready-timeout", 60, "peer readiness timeout in seconds")
	flags.Parse(args)
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: peerkit run [options] <scenario.yaml>")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	run, summary, err := controller.Run(ctx, flags.Arg(0), controller.RunOptions{
		ProjectRoot: *projectRoot, OutputDir: *output, Image: *image,
		NoBuild: *noBuild, Keep: *keep, ReadyTimeoutSeconds: *readyTimeout,
	})
	if err != nil {
		if run != nil {
			log.Printf("partial run data: %s", run.RunDir)
		}
		log.Fatal(err)
	}
	fmt.Printf("run: %s\n", run.RunDir)
	fmt.Printf("messages: %d\n", summary.Messages)
	fmt.Printf("average reachability: %.6f\n", summary.AverageReachability)
	fmt.Printf("average completion delay: %.3f ms\n", summary.AverageCompletionDelayMS)
	fmt.Printf("transmissions: %d, duplicates: %d, drops: %d, suppressions: %d\n",
		summary.TotalTransmissions, summary.TotalDuplicates, summary.TotalDrops, summary.TotalSuppressions)
}

func downCommand(args []string) {
	flags := flag.NewFlagSet("down", flag.ExitOnError)
	timeout := flags.Int("timeout", 30, "shutdown timeout in seconds")
	flags.Parse(args)
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: peerkit down <run-directory>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	if err := controller.Down(ctx, flags.Arg(0)); err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `peerkit - static libp2p propagation experiment tool

Commands:
  validate <scenario.yaml>
  expand [-o resolved.yaml] <scenario.yaml>
  run [options] <scenario.yaml>
  down <run-directory>
  version`)
}
