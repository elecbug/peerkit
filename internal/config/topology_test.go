package config

import "testing"

func TestNormalizeTopologyMatrix(t *testing.T) {
	scenario := Scenario{Topology: TopologyConfig{
		Nodes:  []NodeSpec{{ID: "n0"}, {ID: "n1"}, {ID: "n2"}},
		Matrix: [][]int{{0, 1, 0}, {1, 0, 1}, {0, 1, 0}},
	}}
	if err := scenario.NormalizeTopology(); err != nil {
		t.Fatalf("NormalizeTopology: %v", err)
	}
	if len(scenario.Topology.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(scenario.Topology.Edges))
	}
	if scenario.Topology.Edges[0].Source != "n0" || scenario.Topology.Edges[0].Target != "n1" {
		t.Fatalf("unexpected first edge: %+v", scenario.Topology.Edges[0])
	}
}

func TestNormalizeTopologyRejectsAsymmetry(t *testing.T) {
	scenario := Scenario{Topology: TopologyConfig{
		Nodes:  []NodeSpec{{ID: "n0"}, {ID: "n1"}},
		Matrix: [][]int{{0, 1}, {0, 0}},
	}}
	if err := scenario.NormalizeTopology(); err == nil {
		t.Fatal("expected asymmetric matrix error")
	}
}
