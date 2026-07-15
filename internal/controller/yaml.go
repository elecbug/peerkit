package controller

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func yamlUnmarshal(data []byte, value any) error {
	if err := yaml.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode YAML: %w", err)
	}
	return nil
}
