package cli

import (
	"fmt"
	"io"

	"github.com/k-p2plab/peerkit/internal/controller"
	"github.com/k-p2plab/peerkit/internal/metrics"
)

func (a App) runExperiment(args []string) error {
	flags := newFlagSet("run", a.Stderr)
	projectRoot := flags.String("project-root", ".", "peerkit source tree used as the Docker build context")
	output := flags.String("output", "", "run output directory")
	image := flags.String("image", "peerkit-peer:dev", "peer runtime image")
	noBuild := flags.Bool("no-build", false, "skip image build")
	keep := flags.Bool("keep", false, "keep deployment after a synchronous run")
	detach := flags.Bool("detach", false, "deploy a Swarm run and return immediately")
	readyTimeout := flags.Int("ready-timeout", 180, "Compose peer readiness timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit run [options] <scenario.yaml>")
	}
	ctx, stop := signalContext()
	defer stop()
	run, summary, err := controller.Run(ctx, flags.Arg(0), controller.RunOptions{
		ProjectRoot:         *projectRoot,
		OutputDir:           *output,
		Image:               *image,
		NoBuild:             *noBuild,
		Keep:                *keep,
		Detach:              *detach,
		ReadyTimeoutSeconds: *readyTimeout,
		OnGenerated: func(runDir string) error {
			return saveRecentRun(*projectRoot, runDir)
		},
	})
	if err != nil {
		if run != nil {
			fmt.Fprintf(a.Stderr, "partial run data: %s\n", run.RunDir)
		}
		return err
	}
	fmt.Fprintf(a.Stdout, "run directory: %s\n", run.RunDir)
	if summary == nil {
		fmt.Fprintln(a.Stdout, "deployment started in background")
		fmt.Fprintf(a.Stdout, "next: peerkit status %s\n", run.RunDir)
		fmt.Fprintf(a.Stdout, "collect: peerkit collect %s\n", run.RunDir)
		return nil
	}
	printSummary(a.Stdout, summary)
	return nil
}

func printSummary(writer io.Writer, summary *metrics.RunSummary) {
	fmt.Fprintf(writer, "protocol: %s\n", summary.Protocol)
	fmt.Fprintf(writer, "messages: %d\n", summary.Messages)
	fmt.Fprintf(writer, "average reachability: %.6f\n", summary.AverageReachability)
	fmt.Fprintf(writer, "average completion delay: %.3f ms\n", summary.AverageCompletionDelayMS)
	fmt.Fprintf(writer, "transmissions: %d, duplicates: %d, drops: %d, suppressions: %d\n", summary.TotalTransmissions, summary.TotalDuplicates, summary.TotalDrops, summary.TotalSuppressions)
	if summary.TotalControlSent > 0 || summary.TotalControlDrops > 0 {
		fmt.Fprintf(writer, "control sent: %d, received: %d, drops: %d, bytes: %d\n", summary.TotalControlSent, summary.TotalControlReceived, summary.TotalControlDrops, summary.TotalControlBytesSent)
	}
}
