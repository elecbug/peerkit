package config

import (
	"fmt"
	"math/rand"
)

// ResolveDelayAssignments samples one stable baseline delay for every logical
// node and edge. Runtime processing and transmission delays are then sampled
// from a normal distribution centered on that assigned baseline, using the
// corresponding jitter standard-deviation setting.
func (s *Scenario) ResolveDelayAssignments() error {
	nodeRNG := rand.New(rand.NewSource(domainSeed(s.Experiment.Seed, "node-delay-assignment")))
	for index := range s.Topology.Nodes {
		node := &s.Topology.Nodes[index]
		if node.Performance == nil {
			return fmt.Errorf("node %s has no resolved performance config", node.ID)
		}
		if err := validateDistribution(node.Performance.ProcessingDelayDistribution); err != nil {
			return fmt.Errorf("node %s processing_delay_distribution: %w", node.ID, err)
		}
		if node.Performance.ProcessingDelayJitterStdDevMS < 0 {
			return fmt.Errorf("node %s processing_delay_jitter_stddev must be non-negative", node.ID)
		}
		assigned := node.Performance.ProcessingDelayDistribution.SampleMilliseconds(nodeRNG)
		node.Performance.ProcessingDelayDistribution = constantDistribution(assigned)
	}

	edgeRNG := rand.New(rand.NewSource(domainSeed(s.Experiment.Seed, "edge-delay-assignment")))
	for index := range s.Topology.Edges {
		edge := &s.Topology.Edges[index]
		if edge.Network == nil {
			return fmt.Errorf("edge %s->%s has no resolved network config", edge.Source, edge.Target)
		}
		if err := validateDistribution(edge.Network.DelayDistribution); err != nil {
			return fmt.Errorf("edge %s->%s delay_distribution: %w", edge.Source, edge.Target, err)
		}
		if edge.Network.DelayJitterStdDevMS < 0 {
			return fmt.Errorf("edge %s->%s delay_jitter_stddev must be non-negative", edge.Source, edge.Target)
		}
		assigned := edge.Network.DelayDistribution.SampleMilliseconds(edgeRNG)
		edge.Network.DelayDistribution = constantDistribution(assigned)
	}
	return nil
}
