package config

import (
	"strings"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func (s *Scenario) ApplyDefaults() {
	s.Protocol = protocols.Normalize(s.Protocol)
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Experiment.Name == "" {
		s.Experiment.Name = "peerkit-experiment"
	}
	if s.Experiment.Seed == 0 {
		s.Experiment.Seed = 1
	}
	if s.Experiment.DurationMS <= 0 {
		s.Experiment.DurationMS = 10_000
	}
	if s.Experiment.WarmupMS < 0 {
		s.Experiment.WarmupMS = 0
	}
	if s.Experiment.ControlBasePort == 0 {
		s.Experiment.ControlBasePort = 18_080
	}
	if s.Controller.Parallelism == 0 {
		s.Controller.Parallelism = 32
	}
	if s.Controller.OperationTimeoutSeconds == 0 {
		s.Controller.OperationTimeoutSeconds = 180
	}
	if s.Metrics.BufferBytes == 0 {
		s.Metrics.BufferBytes = 256 * 1024
	}
	if s.Metrics.QueueCapacity == 0 {
		s.Metrics.QueueCapacity = 512
	}
	if s.Metrics.FlushIntervalMS == 0 {
		s.Metrics.FlushIntervalMS = 200
	}
	if s.Deployment.Mode == "" {
		s.Deployment.Mode = "compose"
	}
	s.Deployment.Mode = strings.ToLower(strings.TrimSpace(s.Deployment.Mode))
	if s.Deployment.ComposeParallelism == 0 {
		s.Deployment.ComposeParallelism = 4
	}
	if s.Deployment.Swarm.PushImage == nil {
		v := true
		s.Deployment.Swarm.PushImage = &v
	}
	if s.Deployment.Swarm.WithRegistryAuth == nil {
		v := true
		s.Deployment.Swarm.WithRegistryAuth = &v
	}
	if s.Deployment.Swarm.StartupTimeoutSeconds == 0 {
		s.Deployment.Swarm.StartupTimeoutSeconds = 600
	}
	if s.Deployment.Swarm.StartupBatchSize == 0 {
		s.Deployment.Swarm.StartupBatchSize = 25
	}
	if s.Deployment.Swarm.StartupBatchIntervalMS == 0 {
		s.Deployment.Swarm.StartupBatchIntervalMS = 1000
	}

	applyNodeDefaults(&s.Defaults.Node)
	applyEdgeDefaults(&s.Defaults.Edge)

	for i := range s.Topology.Nodes {
		if s.Topology.Nodes[i].Performance == nil {
			p := s.Defaults.Node
			s.Topology.Nodes[i].Performance = &p
			continue
		}
		mergeNodePerformance(s.Topology.Nodes[i].Performance, s.Defaults.Node)
	}

	for i := range s.Topology.Edges {
		if s.Topology.Edges[i].Network == nil {
			n := s.Defaults.Edge
			s.Topology.Edges[i].Network = &n
			continue
		}
		mergeEdgeNetwork(s.Topology.Edges[i].Network, s.Defaults.Edge)
	}

	for i := range s.Traffic {
		if s.Traffic[i].Count == 0 {
			s.Traffic[i].Count = 1
		}
		if s.Traffic[i].PayloadSizeBytes == 0 {
			s.Traffic[i].PayloadSizeBytes = 1024
		}
	}
}

func applyNodeDefaults(p *NodePerformance) {
	if p.ProcessingDelay.Type == "" {
		p.ProcessingDelay = Distribution{Type: "constant", ValueMS: 0}
	}
	if p.Workers == 0 {
		p.Workers = 1
	}
	if p.QueueCapacity == 0 {
		p.QueueCapacity = 1024
	}
	if p.OverflowPolicy == "" {
		p.OverflowPolicy = "drop_new"
	}
	p.OverflowPolicy = strings.ToLower(p.OverflowPolicy)
}

func mergeNodePerformance(dst *NodePerformance, defaults NodePerformance) {
	if dst.ProcessingDelay.Type == "" {
		dst.ProcessingDelay = defaults.ProcessingDelay
	}
	if dst.Workers == 0 {
		dst.Workers = defaults.Workers
	}
	if dst.QueueCapacity == 0 {
		dst.QueueCapacity = defaults.QueueCapacity
	}
	if dst.OverflowPolicy == "" {
		dst.OverflowPolicy = defaults.OverflowPolicy
	}
	applyNodeDefaults(dst)
}

func applyEdgeDefaults(n *EdgeNetwork) {
	if n.Delay.Type == "" {
		n.Delay = Distribution{Type: "constant", ValueMS: 0}
	}
	if n.LossRate == nil {
		v := 0.0
		n.LossRate = &v
	}
	if n.BandwidthMbps == nil {
		v := 0.0
		n.BandwidthMbps = &v
	}
	if n.QueueCapacity == 0 {
		n.QueueCapacity = 1024
	}
}

func mergeEdgeNetwork(dst *EdgeNetwork, defaults EdgeNetwork) {
	if dst.Delay.Type == "" {
		dst.Delay = defaults.Delay
	}
	if dst.LossRate == nil {
		v := *defaults.LossRate
		dst.LossRate = &v
	}
	if dst.BandwidthMbps == nil {
		v := *defaults.BandwidthMbps
		dst.BandwidthMbps = &v
	}
	if dst.QueueCapacity == 0 {
		dst.QueueCapacity = defaults.QueueCapacity
	}
	applyEdgeDefaults(dst)
}

func (n EdgeNetwork) Resolve() ResolvedEdgeNetwork {
	loss := 0.0
	if n.LossRate != nil {
		loss = *n.LossRate
	}
	bandwidth := 0.0
	if n.BandwidthMbps != nil {
		bandwidth = *n.BandwidthMbps
	}
	return ResolvedEdgeNetwork{
		Delay:         n.Delay,
		LossRate:      loss,
		BandwidthMbps: bandwidth,
		QueueCapacity: n.QueueCapacity,
	}
}
