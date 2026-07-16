package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/k-p2plab/peerkit/internal/controller"
)

func (a App) status(args []string) error {
	flags := newFlagSet("status", a.Stderr)
	controllerURL := flags.String("controller-url", "", "override the Controller URL stored in run.yaml")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON")
	timeout := flags.Int("timeout", 15, "status request timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit status [options] <run-directory>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	report, err := controller.InspectRun(ctx, flags.Arg(0), *controllerURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(a.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Fprintf(a.Stdout, "run: %s\n", report.RunDir)
	fmt.Fprintf(a.Stdout, "deployment: %s\n", report.DeploymentMode)
	fmt.Fprintf(a.Stdout, "project: %s\n", report.ProjectName)
	if report.ControllerURL != "" {
		fmt.Fprintf(a.Stdout, "controller: %s\n", report.ControllerURL)
	}
	if report.ControllerError != "" {
		fmt.Fprintf(a.Stdout, "controller error: %s\n", report.ControllerError)
	}
	if report.Controller != nil {
		fmt.Fprintf(a.Stdout, "state: %s\n", report.Controller.State)
		fmt.Fprintf(a.Stdout, "registered peers: %d/%d\n", report.Controller.Registered, report.Controller.Expected)
		if report.Controller.Error != "" {
			fmt.Fprintf(a.Stdout, "controller error: %s\n", report.Controller.Error)
		}
	}
	if report.RuntimeSummary != "" {
		fmt.Fprintln(a.Stdout, "runtime:")
		fmt.Fprintln(a.Stdout, report.RuntimeSummary)
	}
	return nil
}

func (a App) collect(args []string) error {
	flags := newFlagSet("collect", a.Stderr)
	controllerURL := flags.String("controller-url", "", "override the Controller URL stored in run.yaml")
	timeout := flags.Int("timeout", 1800, "maximum wait for completion in seconds")
	poll := flags.Duration("poll", 2*time.Second, "Controller polling interval")
	removeTimeout := flags.Int("remove-timeout", 180, "maximum cleanup wait in seconds")
	keep := flags.Bool("keep", false, "keep services and network after collection")
	removeOnFailure := flags.Bool("remove-on-failure", false, "remove the deployment even if collection fails")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit collect [options] <run-directory>")
	}
	ctx, stop := signalContext()
	defer stop()
	status, err := controller.CollectRun(ctx, flags.Arg(0), controller.CollectOptions{
		ControllerURL:   *controllerURL,
		Timeout:         time.Duration(*timeout) * time.Second,
		PollInterval:    *poll,
		RemoveTimeout:   time.Duration(*removeTimeout) * time.Second,
		KeepDeployment:  *keep,
		RemoveOnFailure: *removeOnFailure,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "results: %s\n", filepath.Join(flags.Arg(0), "results"))
	fmt.Fprintf(a.Stdout, "archive: %s\n", filepath.Join(flags.Arg(0), "peerkit-results.tar.gz"))
	if status != nil && status.Summary != nil {
		printSummary(a.Stdout, status.Summary)
	}
	if *keep {
		fmt.Fprintln(a.Stdout, "deployment retained")
	} else {
		fmt.Fprintln(a.Stdout, "services and stack networks removed")
	}
	return nil
}

func (a App) logs(args []string) error {
	flags := newFlagSet("logs", a.Stderr)
	service := flags.String("service", "all", "Swarm: controller, peers, or all; Compose: peers, all, or a peer service name")
	tail := flags.Int("tail", 100, "number of lines")
	follow := flags.Bool("follow", false, "follow log output")
	flags.BoolVar(follow, "f", false, "alias for --follow")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit logs [options] <run-directory>")
	}
	ctx, stop := signalContext()
	defer stop()
	return controller.StreamRunLogs(ctx, flags.Arg(0), *service, *tail, *follow, a.Stdout, a.Stderr)
}

func (a App) stop(args []string) error {
	flags := newFlagSet("stop", a.Stderr)
	timeout := flags.Int("timeout", 180, "cleanup timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit stop [--timeout seconds] <run-directory>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	if err := controller.RemoveDeployment(ctx, flags.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "deployment removed")
	return nil
}

func (a App) diagnose(args []string) error {
	flags := newFlagSet("diagnose", a.Stderr)
	timeout := flags.Int("timeout", 60, "diagnostic collection timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit diagnose <run-directory>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	if err := controller.SaveRunDiagnostics(ctx, flags.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "diagnostics: %s\n", filepath.Join(flags.Arg(0), "diagnostics"))
	return nil
}
