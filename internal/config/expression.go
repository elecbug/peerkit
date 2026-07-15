package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type distributionAlias Distribution

// UnmarshalYAML accepts both the original mapping form and compact expressions:
//
//	normal(mean=100ms, stddev=20ms)
//	normal(100ms, 20ms)
//	exponential(25ms)
//	constant(0ms)
//	uniform(10ms, 50ms)
//	pareto(scale=20ms, shape=2.5)
func (d *Distribution) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		var decoded distributionAlias
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		*d = Distribution(decoded)
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("distribution must be a mapping or expression string")
	}

	parsed, err := ParseDistributionExpression(node.Value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func ParseDistributionExpression(expression string) (Distribution, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return Distribution{}, fmt.Errorf("distribution expression is empty")
	}

	// A bare duration or number is shorthand for constant(...).
	if !strings.Contains(expression, "(") {
		value, err := parseMilliseconds(expression)
		if err != nil {
			return Distribution{}, fmt.Errorf("invalid distribution expression %q: %w", expression, err)
		}
		return Distribution{Type: "constant", ValueMS: value}, nil
	}

	open := strings.IndexByte(expression, '(')
	if open <= 0 || !strings.HasSuffix(expression, ")") {
		return Distribution{}, fmt.Errorf("invalid distribution expression %q", expression)
	}
	name := strings.ToLower(strings.TrimSpace(expression[:open]))
	body := strings.TrimSpace(expression[open+1 : len(expression)-1])
	args, named, err := parseExpressionArguments(body)
	if err != nil {
		return Distribution{}, fmt.Errorf("invalid %s expression: %w", name, err)
	}

	switch name {
	case "constant", "fixed":
		value, err := durationArgument(args, named, 0, "value", "value_ms")
		if err != nil {
			return Distribution{}, err
		}
		return Distribution{Type: "constant", ValueMS: value}, nil

	case "uniform":
		minValue, err := durationArgument(args, named, 0, "min", "min_ms")
		if err != nil {
			return Distribution{}, err
		}
		maxValue, err := durationArgument(args, named, 1, "max", "max_ms")
		if err != nil {
			return Distribution{}, err
		}
		return Distribution{Type: "uniform", MinMS: minValue, MaxMS: maxValue}, nil

	case "normal", "gaussian":
		mean, err := durationArgument(args, named, 0, "mean", "mean_ms", "mu", "μ")
		if err != nil {
			return Distribution{}, err
		}
		stddev, err := durationArgument(args, named, 1, "stddev", "stddev_ms", "sigma", "σ")
		if err != nil {
			return Distribution{}, err
		}
		return Distribution{Type: "normal", MeanMS: mean, StdDevMS: stddev}, nil

	case "exponential", "exp":
		mean, err := durationArgument(args, named, 0, "mean", "mean_ms")
		if err != nil {
			return Distribution{}, err
		}
		return Distribution{Type: "exponential", MeanMS: mean}, nil

	case "pareto":
		scale, err := durationArgument(args, named, 0, "scale", "scale_ms", "xm")
		if err != nil {
			return Distribution{}, err
		}
		shapeValue, err := scalarArgument(args, named, 1, "shape", "alpha", "α")
		if err != nil {
			return Distribution{}, err
		}
		return Distribution{Type: "pareto", ScaleMS: scale, Shape: shapeValue}, nil

	default:
		return Distribution{}, fmt.Errorf("unsupported distribution expression %q", name)
	}
}

func parseExpressionArguments(body string) ([]string, map[string]string, error) {
	if body == "" {
		return nil, map[string]string{}, nil
	}
	parts := strings.Split(body, ",")
	positional := make([]string, 0, len(parts))
	named := make(map[string]string)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, nil, fmt.Errorf("empty argument")
		}
		if equals := strings.IndexByte(part, '='); equals >= 0 {
			key := strings.ToLower(strings.TrimSpace(part[:equals]))
			value := strings.TrimSpace(part[equals+1:])
			if key == "" || value == "" {
				return nil, nil, fmt.Errorf("invalid named argument %q", part)
			}
			if _, exists := named[key]; exists {
				return nil, nil, fmt.Errorf("duplicate argument %q", key)
			}
			named[key] = value
			continue
		}
		positional = append(positional, part)
	}
	return positional, named, nil
}

func durationArgument(positional []string, named map[string]string, index int, keys ...string) (float64, error) {
	value, ok := lookupArgument(positional, named, index, keys...)
	if !ok {
		return 0, fmt.Errorf("missing argument %s", strings.Join(keys, "/"))
	}
	milliseconds, err := parseMilliseconds(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", keys[0], value, err)
	}
	return milliseconds, nil
}

func scalarArgument(positional []string, named map[string]string, index int, keys ...string) (float64, error) {
	value, ok := lookupArgument(positional, named, index, keys...)
	if !ok {
		return 0, fmt.Errorf("missing argument %s", strings.Join(keys, "/"))
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", keys[0], value, err)
	}
	return number, nil
}

func lookupArgument(positional []string, named map[string]string, index int, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := named[key]; ok {
			return value, true
		}
	}
	if index < len(positional) {
		return positional[index], true
	}
	return "", false
}

func parseMilliseconds(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return float64(duration) / float64(time.Millisecond), nil
}
