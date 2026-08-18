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
	if err := resolveNodeDelayAssignments(s.Topology.Nodes, s.Experiment.Seed); err != nil {
		return err
	}
	return resolveEdgeDelayAssignments(s.Topology.Edges, s.Experiment.Seed)
}

// resolveNodeDelayAssignments resolves node processing baselines independently
// from topology generation. Performance-aware generators such as
// BA-opportunistic and DRS use the same deterministic assignment before
// topology construction so node performance can affect placement without
// changing the baseline sampling semantics.
func resolveNodeDelayAssignments(nodes []NodeSpec, seed int64) error {
	nodeRNG := rand.New(rand.NewSource(domainSeed(seed, "node-delay-assignment")))
	for index := range nodes {
		node := &nodes[index]
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
	return nil
}

func resolveEdgeDelayAssignments(edges []EdgeSpec, seed int64) error {
	edgeRNG := rand.New(rand.NewSource(domainSeed(seed, "edge-delay-assignment")))
	for index := range edges {
		edge := &edges[index]
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
