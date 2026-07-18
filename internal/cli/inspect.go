package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/k-p2plab/peerkit/internal/controller"
)

func (a App) inspect(args []string) error {
	flags := newFlagSet("inspect", a.Stderr)
	projectRoot := flags.String("project-root", ".", "peerkit project root containing bin/.peerkit-last-run")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON")
	timeout := flags.Int("timeout", 60, "inspection timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: peerkit inspect [options] [run-directory]")
	}
	runDir := ""
	if flags.NArg() == 1 {
		runDir = flags.Arg(0)
	} else {
		var err error
		runDir, err = loadRecentRun(*projectRoot)
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	report, err := controller.InspectDeployment(ctx, runDir)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(a.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Fprintf(a.Stdout, "PEERKIT DEPLOYMENT INSPECT\n")
	fmt.Fprintf(a.Stdout, "run: %s\n", report.RunDir)
	fmt.Fprintf(a.Stdout, "deployment: %s\n", report.DeploymentMode)
	fmt.Fprintf(a.Stdout, "project: %s\n", report.ProjectName)
	fmt.Fprintf(a.Stdout, "overall: %s\n\n", strings.ToUpper(string(report.Overall)))
	for _, finding := range report.Findings {
		fmt.Fprintf(a.Stdout, "[%s] %s: %s\n", strings.ToUpper(string(finding.Severity)), finding.Category, finding.Summary)
		for key, value := range finding.Details {
			fmt.Fprintf(a.Stdout, "  %s: %v\n", key, value)
		}
		for _, advice := range finding.Advice {
			fmt.Fprintf(a.Stdout, "  advice: %s\n", advice)
		}
		fmt.Fprintln(a.Stdout)
	}
	return nil
}
