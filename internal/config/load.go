package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario: %w", err)
	}

	var scenario Scenario
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&scenario); err != nil {
		return nil, fmt.Errorf("decode scenario: %w", err)
	}

	// The first pass resolves experiment and top-level defaults needed by domain
	// expansion. The second pass applies those defaults to generated nodes/edges.
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err != nil {
		return nil, err
	}
	if err := scenario.NormalizeTopology(); err != nil {
		return nil, err
	}
	scenario.ApplyDefaults()
	if err := scenario.Validate(); err != nil {
		return nil, err
	}
	return &scenario, nil
}

func LoadRuntimeNode(path string) (*RuntimeNodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read runtime node config: %w", err)
	}

	var cfg RuntimeNodeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode runtime node config: %w", err)
	}
	return &cfg, nil
}

func WriteYAML(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode YAML: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
