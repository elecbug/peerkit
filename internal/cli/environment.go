package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/k-p2plab/peerkit/internal/controller"
)

func (a App) doctor(args []string) error {
	flags := newFlagSet("doctor", a.Stderr)
	mode := flags.String("mode", "compose", "compose or swarm")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: peerkit doctor [--mode compose|swarm]")
	}
	checks := []struct {
		name string
		args []string
	}{
		{"Docker Engine", []string{"docker", "version", "--format", "{{.Server.Version}}"}},
	}
	if *mode == "compose" {
		checks = append(checks, struct {
			name string
			args []string
		}{"Docker Compose", []string{"docker", "compose", "version", "--short"}})
	} else if *mode == "swarm" {
		checks = append(checks, struct {
			name string
			args []string
		}{"Swarm manager", []string{"docker", "info", "--format", "{{.Swarm.LocalNodeState}} {{.Swarm.ControlAvailable}}"}})
	} else {
		return fmt.Errorf("unknown mode %q", *mode)
	}
	failed := false
	for _, check := range checks {
		command := exec.Command(check.args[0], check.args[1:]...)
		output, err := command.CombinedOutput()
		if err != nil {
			failed = true
			fmt.Fprintf(a.Stdout, "FAIL  %s: %s\n", check.name, strings.TrimSpace(string(output)))
			continue
		}
		value := strings.TrimSpace(string(output))
		if check.name == "Swarm manager" && value != "active true" {
			failed = true
			fmt.Fprintf(a.Stdout, "FAIL  %s: %s\n", check.name, value)
			continue
		}
		fmt.Fprintf(a.Stdout, "OK    %s: %s\n", check.name, value)
	}
	if failed {
		return fmt.Errorf("one or more environment checks failed")
	}
	return nil
}

func (a App) image(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: peerkit image <build|push> [options]")
	}
	subcommand := args[0]
	flags := newFlagSet("image "+subcommand, a.Stderr)
	projectRoot := flags.String("project-root", ".", "peerkit source tree")
	tag := flags.String("tag", "peerkit-peer:dev", "image tag")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected image arguments: %s", strings.Join(flags.Args(), " "))
	}
	ctx, stop := signalContext()
	defer stop()
	switch subcommand {
	case "build":
		if err := controller.BuildImage(ctx, *projectRoot, *tag); err != nil {
			return err
		}
	case "push":
		if err := controller.PushImage(ctx, *projectRoot, *tag); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown image command %q", subcommand)
	}
	fmt.Fprintf(a.Stdout, "image: %s\n", *tag)
	return nil
}
