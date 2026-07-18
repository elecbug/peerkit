package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os/signal"
	"syscall"
)

const Version = "0.8.0"

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func Run(args []string, stdout, stderr io.Writer) int {
	app := App{Stdout: stdout, Stderr: stderr}
	if err := app.run(args); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func (a App) run(args []string) error {
	if len(args) == 0 {
		a.usage()
		return nil
	}
	log.SetOutput(a.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	switch args[0] {
	case "help", "-h", "--help":
		a.usage()
		return nil
	case "version":
		fmt.Fprintln(a.Stdout, Version)
		return nil
	case "validate":
		return a.validate(args[1:])
	case "expand":
		return a.expand(args[1:])
	case "run":
		return a.runExperiment(args[1:])
	case "status":
		return a.status(args[1:])
	case "inspect":
		return a.inspect(args[1:])
	case "collect":
		return a.collect(args[1:])
	case "logs":
		return a.logs(args[1:])
	case "stop", "down":
		return a.stop(args[1:])
	case "diagnose":
		return a.diagnose(args[1:])
	case "doctor":
		return a.doctor(args[1:])
	case "image":
		return a.image(args[1:])
	case "examples":
		return a.examples(args[1:])
	default:
		a.usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	return flags
}

func (a App) usage() {
	fmt.Fprintln(a.Stdout, `peerkit - static libp2p propagation experiment tool

Usage:
  peerkit <command> [options]

Experiment commands:
  validate <scenario.yaml>              Validate and resolve a scenario
  expand [-o file] <scenario.yaml>      Write the explicit resolved topology
  run [options] <scenario.yaml>         Build, deploy, execute, collect, and clean up
  status [options] <run-directory>      Show Controller and runtime status
  inspect [options] [run-directory]     Diagnose services, tasks, networks, ports, and Swarm health
  collect [options] <run-directory>     Resume Swarm collection and clean up
  logs [options] <run-directory>        Show Controller or peer logs
  stop [run-directory]                  Remove a run; defaults to bin/.peerkit-last-run
  diagnose <run-directory>              Save runtime diagnostics

Environment commands:
  doctor [--mode compose|swarm]         Verify Docker prerequisites
  image build [--tag image]             Build the peer runtime image
  image push [--tag image]              Push the peer runtime image
  examples                              List bundled YAML examples
  version                               Print the peerkit version

Typical workflows:
  peerkit run examples/compose/03-domain-er-base.yaml
  peerkit run --detach --image registry.local:5000/peerkit:dev examples/swarm/02-multi-node-registry.yaml
  peerkit status .peerkit/runs/<run>
  peerkit inspect                       # inspect the most recent run
  peerkit stop                          # stop the most recent run
  peerkit collect .peerkit/runs/<run>`)
}
