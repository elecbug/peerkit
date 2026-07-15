package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func (s *Scenario) Validate() error {
	if err := protocols.Validate(s.Protocol); err != nil {
		return err
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported scenario version %d", s.Version)
	}
	if s.Topology.Directed {
		return fmt.Errorf("directed topology is not supported in peerkit v0.1")
	}
	if len(s.Topology.Nodes) == 0 {
		return fmt.Errorf("topology.nodes must not be empty")
	}
	if s.Experiment.ControlBasePort < 1024 || s.Experiment.ControlBasePort+len(s.Topology.Nodes) > 65535 {
		return fmt.Errorf("control port range is invalid")
	}
	if s.Controller.Parallelism <= 0 {
		return fmt.Errorf("controller.parallelism must be positive")
	}
	if s.Controller.OperationTimeoutSeconds <= 0 {
		return fmt.Errorf("controller.operation_timeout_seconds must be positive")
	}
	if s.Metrics.BufferBytes < 4096 {
		return fmt.Errorf("metrics.buffer_bytes must be at least 4096")
	}
	if s.Metrics.QueueCapacity <= 0 {
		return fmt.Errorf("metrics.queue_capacity must be positive")
	}
	if s.Metrics.FlushIntervalMS <= 0 {
		return fmt.Errorf("metrics.flush_interval_ms must be positive")
	}

	nodes := make(map[string]int, len(s.Topology.Nodes))
	for i, node := range s.Topology.Nodes {
		if !nodeIDPattern.MatchString(node.ID) {
			return fmt.Errorf("node %d has invalid id %q", i, node.ID)
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate node id %q", node.ID)
		}
		nodes[node.ID] = i
		if node.Performance == nil {
			return fmt.Errorf("node %q has no resolved performance config", node.ID)
		}
		if err := validateNodePerformance(*node.Performance); err != nil {
			return fmt.Errorf("node %q: %w", node.ID, err)
		}
		if node.Resources != nil {
			if node.Resources.CPULimit < 0 {
				return fmt.Errorf("node %q cpu_limit must be non-negative", node.ID)
			}
			if node.Resources.MemoryLimitMB < 0 {
				return fmt.Errorf("node %q memory_limit_mb must be non-negative", node.ID)
			}
		}
	}

	edges := make(map[string]struct{}, len(s.Topology.Edges))
	for i, edge := range s.Topology.Edges {
		if _, ok := nodes[edge.Source]; !ok {
			return fmt.Errorf("edge %d references unknown source %q", i, edge.Source)
		}
		if _, ok := nodes[edge.Target]; !ok {
			return fmt.Errorf("edge %d references unknown target %q", i, edge.Target)
		}
		if edge.Source == edge.Target {
			return fmt.Errorf("edge %d is a self-loop on %q", i, edge.Source)
		}
		key := undirectedEdgeKey(edge.Source, edge.Target)
		if _, exists := edges[key]; exists {
			return fmt.Errorf("duplicate undirected edge %s", key)
		}
		edges[key] = struct{}{}
		if edge.Network == nil {
			return fmt.Errorf("edge %s has no resolved network config", key)
		}
		if err := validateEdgeNetwork(edge.Network.Resolve()); err != nil {
			return fmt.Errorf("edge %s: %w", key, err)
		}
	}

	for i, traffic := range s.Traffic {
		if !IsRandomTrafficSource(traffic.Source) {
			if _, ok := nodes[traffic.Source]; !ok {
				return fmt.Errorf("traffic %d references unknown source %q", i, traffic.Source)
			}
		}
		if traffic.StartAtMS < 0 || traffic.IntervalMS < 0 {
			return fmt.Errorf("traffic %d time values must be non-negative", i)
		}
		if traffic.Count <= 0 {
			return fmt.Errorf("traffic %d count must be positive", i)
		}
		if traffic.PayloadSizeBytes < 0 {
			return fmt.Errorf("traffic %d payload_size_bytes must be non-negative", i)
		}
		lastEmission := traffic.StartAtMS + int64(traffic.Count-1)*traffic.IntervalMS
		if lastEmission > s.Experiment.DurationMS {
			return fmt.Errorf("traffic %d emits its last message at %d ms, after duration_ms=%d", i, lastEmission, s.Experiment.DurationMS)
		}
	}
	return nil
}

func validateNodePerformance(p NodePerformance) error {
	if p.Workers <= 0 {
		return fmt.Errorf("workers must be positive")
	}
	if p.QueueCapacity <= 0 {
		return fmt.Errorf("queue_capacity must be positive")
	}
	if p.OverflowPolicy != "drop_new" {
		return fmt.Errorf("unsupported overflow_policy %q; peerkit v0.1 supports drop_new", p.OverflowPolicy)
	}
	return validateDistribution(p.ProcessingDelay)
}

func validateEdgeNetwork(n ResolvedEdgeNetwork) error {
	if n.LossRate < 0 || n.LossRate > 1 {
		return fmt.Errorf("loss_rate must be between 0 and 1")
	}
	if n.BandwidthMbps < 0 {
		return fmt.Errorf("bandwidth_mbps must be non-negative")
	}
	if n.QueueCapacity <= 0 {
		return fmt.Errorf("queue_capacity must be positive")
	}
	return validateDistribution(n.Delay)
}

func validateDistribution(d Distribution) error {
	switch strings.ToLower(d.Type) {
	case "constant":
		if d.ValueMS < 0 {
			return fmt.Errorf("constant value_ms must be non-negative")
		}
	case "uniform":
		if d.MinMS < 0 || d.MaxMS < d.MinMS {
			return fmt.Errorf("uniform requires 0 <= min_ms <= max_ms")
		}
	case "normal":
		if d.MeanMS < 0 || d.StdDevMS < 0 {
			return fmt.Errorf("normal mean_ms and stddev_ms must be non-negative")
		}
	case "exponential":
		if d.MeanMS < 0 {
			return fmt.Errorf("exponential mean_ms must be non-negative")
		}
	case "pareto":
		if d.ScaleMS < 0 || d.Shape <= 0 {
			return fmt.Errorf("pareto requires scale_ms >= 0 and shape > 0")
		}
	default:
		return fmt.Errorf("unsupported distribution %q", d.Type)
	}
	return nil
}

func undirectedEdgeKey(a, b string) string {
	if strings.Compare(a, b) < 0 {
		return a + "--" + b
	}
	return b + "--" + a
}

// IsRandomTrafficSource reports whether a traffic source requests uniform
// per-message source selection across all topology nodes.
func IsRandomTrafficSource(source string) bool {
	return strings.EqualFold(strings.TrimSpace(source), RandomTrafficSource)
}
