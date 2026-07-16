package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/k-p2plab/peerkit/internal/config"
)

func (a App) validate(args []string) error {
	flags := newFlagSet("validate", a.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit validate <scenario.yaml>")
	}
	scenario, err := config.LoadScenario(flags.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "valid: %s\n", scenario.Experiment.Name)
	fmt.Fprintf(a.Stdout, "deployment: %s\n", scenario.Deployment.Mode)
	fmt.Fprintf(a.Stdout, "protocol: %s\n", scenario.Protocol)
	fmt.Fprintf(a.Stdout, "topology: %d nodes, %d edges\n", len(scenario.Topology.Nodes), len(scenario.Topology.Edges))
	fmt.Fprintf(a.Stdout, "traffic patterns: %d\n", len(scenario.Traffic))
	return nil
}

func (a App) expand(args []string) error {
	flags := newFlagSet("expand", a.Stderr)
	output := flags.String("output", "", "write the resolved explicit scenario to this file")
	flags.StringVar(output, "o", "", "alias for --output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: peerkit expand [-o resolved.yaml] <scenario.yaml>")
	}
	scenario, err := config.LoadScenario(flags.Arg(0))
	if err != nil {
		return err
	}
	resolved := *scenario
	resolved.Domain = nil
	data, err := yaml.Marshal(&resolved)
	if err != nil {
		return err
	}
	if *output == "" {
		_, err = a.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "resolved scenario: %s (%d nodes, %d edges)\n", *output, len(resolved.Topology.Nodes), len(resolved.Topology.Edges))
	return nil
}

func (a App) examples(args []string) error {
	flags := newFlagSet("examples", a.Stderr)
	root := flags.String("root", "examples", "examples directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: peerkit examples [--root examples]")
	}
	var values []string
	err := filepath.WalkDir(*root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			values = append(values, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(values)
	for _, value := range values {
		fmt.Fprintln(a.Stdout, value)
	}
	return nil
}
