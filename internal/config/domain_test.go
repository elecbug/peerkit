package config

import (
	"reflect"
	"testing"
)

func floatPointer(value float64) *float64 {
	return &value
}

func domainScenario(model DomainTopologyConfig, count int) *Scenario {
	return &Scenario{
		Version: 1,
		Experiment: ExperimentConfig{
			Name:            "domain-test",
			Seed:            42,
			DurationMS:      1000,
			ControlBasePort: 18080,
		},
		Domain: &DomainConfig{
			N:        count,
			IDPrefix: "n",
			Topology: model,
			Node: &NodePerformance{
				ProcessingDelay: Distribution{Type: "constant", ValueMS: 1},
				Workers:         1, QueueCapacity: 16, OverflowPolicy: "drop_new",
			},
			Edge: &EdgeNetwork{
				Delay:         Distribution{Type: "constant", ValueMS: 1},
				QueueCapacity: 16,
			},
		},
	}
}

func resolveDomainForTest(t *testing.T, scenario *Scenario) {
	t.Helper()
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err != nil {
		t.Fatal(err)
	}
	scenario.ApplyDefaults()
	if err := scenario.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestERDomainIsDeterministicAndConnected(t *testing.T) {
	model := DomainTopologyConfig{
		Model: "er", P: floatPointer(0.03), EnsureConnected: true,
	}
	first := domainScenario(model, 50)
	second := domainScenario(model, 50)
	resolveDomainForTest(t, first)
	resolveDomainForTest(t, second)

	if !reflect.DeepEqual(first.Topology.Edges, second.Topology.Edges) {
		t.Fatal("same seed and domain generated different ER graphs")
	}
	if len(generatedComponentsFromScenario(first)) != 1 {
		t.Fatal("ensure_connected did not produce one connected component")
	}
	if first.Topology.Nodes[0].ID != "n00" || first.Topology.Nodes[49].ID != "n49" {
		t.Fatalf("unexpected generated ids: %s ... %s", first.Topology.Nodes[0].ID, first.Topology.Nodes[49].ID)
	}
}

func TestBADomainEdgeCount(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{Model: "ba", M: 2}, 20)
	resolveDomainForTest(t, scenario)

	// Initial clique K_(m+1), followed by m edges for each remaining node.
	want := 3 + (20-3)*2
	if len(scenario.Topology.Edges) != want {
		t.Fatalf("BA edge count=%d; want %d", len(scenario.Topology.Edges), want)
	}
}

func TestWSDomainPreservesEdgeCount(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{
		Model: "ws", K: 4, Beta: floatPointer(0.5),
	}, 20)
	resolveDomainForTest(t, scenario)

	want := 20 * 4 / 2
	if len(scenario.Topology.Edges) != want {
		t.Fatalf("WS edge count=%d; want %d", len(scenario.Topology.Edges), want)
	}
}

func generatedComponentsFromScenario(scenario *Scenario) [][]int {
	indices := make(map[string]int, len(scenario.Topology.Nodes))
	for index, node := range scenario.Topology.Nodes {
		indices[node.ID] = index
	}
	edges := make([]generatedEdge, 0, len(scenario.Topology.Edges))
	for _, edge := range scenario.Topology.Edges {
		edges = append(edges, generatedEdge{a: indices[edge.Source], b: indices[edge.Target]})
	}
	return generatedComponents(len(scenario.Topology.Nodes), edges)
}

func TestDomainNormalDelayAssignsPerNodeMeans(t *testing.T) {
	model := DomainTopologyConfig{Model: "path"}
	first := domainScenario(model, 12)
	first.Domain.Node.ProcessingDelay = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}
	second := domainScenario(model, 12)
	second.Domain.Node.ProcessingDelay = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}

	resolveDomainForTest(t, first)
	resolveDomainForTest(t, second)

	means := make(map[float64]struct{})
	for i, node := range first.Topology.Nodes {
		got := node.Performance.ProcessingDelay
		if got.Type != "normal" || got.StdDevMS != 25 {
			t.Fatalf("node %d delay=%+v; want normal with stddev 25", i, got)
		}
		if got.MeanMS < 0 {
			t.Fatalf("node %d has negative mean %f", i, got.MeanMS)
		}
		means[got.MeanMS] = struct{}{}
		if got != second.Topology.Nodes[i].Performance.ProcessingDelay {
			t.Fatalf("node %d mean assignment is not deterministic", i)
		}
	}
	if len(means) == 1 {
		t.Fatal("all generated nodes received the same mean")
	}
}

func TestDomainNodeMeanSamplingDoesNotChangeTopology(t *testing.T) {
	model := DomainTopologyConfig{Model: "er", P: floatPointer(0.08), EnsureConnected: true}
	constant := domainScenario(model, 40)
	normal := domainScenario(model, 40)
	normal.Domain.Node.ProcessingDelay = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}

	resolveDomainForTest(t, constant)
	resolveDomainForTest(t, normal)
	if !reflect.DeepEqual(constant.Topology.Edges, normal.Topology.Edges) {
		t.Fatal("node performance sampling changed the generated topology")
	}
}
