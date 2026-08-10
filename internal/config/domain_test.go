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
				ProcessingDelayDistribution: Distribution{Type: "constant", ValueMS: 1},
				Workers:                     1, QueueCapacity: 16, OverflowPolicy: "drop_new",
			},
			Edge: &EdgeNetwork{
				DelayDistribution: Distribution{Type: "constant", ValueMS: 1},
				QueueCapacity:     16,
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
	if err := scenario.ResolveDelayAssignments(); err != nil {
		t.Fatal(err)
	}
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

func TestDomainDelayDistributionAssignsPerNodeMeans(t *testing.T) {
	model := DomainTopologyConfig{Model: "path"}
	first := domainScenario(model, 12)
	first.Domain.Node.ProcessingDelayDistribution = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}
	first.Domain.Node.ProcessingDelayJitterStdDevMS = 8
	second := domainScenario(model, 12)
	second.Domain.Node.ProcessingDelayDistribution = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}
	second.Domain.Node.ProcessingDelayJitterStdDevMS = 8

	resolveDomainForTest(t, first)
	resolveDomainForTest(t, second)

	means := make(map[float64]struct{})
	for i, node := range first.Topology.Nodes {
		got := node.Performance.ProcessingDelayDistribution
		if got.Type != "constant" {
			t.Fatalf("node %d delay=%+v; want assigned constant baseline", i, got)
		}
		if got.ValueMS < 0 {
			t.Fatalf("node %d has negative assigned delay %f", i, got.ValueMS)
		}
		if node.Performance.ProcessingDelayJitterStdDevMS != 8 {
			t.Fatalf("node %d processing_delay_jitter_stddev=%v; want 8ms", i, node.Performance.ProcessingDelayJitterStdDevMS)
		}
		means[got.ValueMS] = struct{}{}
		if got != second.Topology.Nodes[i].Performance.ProcessingDelayDistribution {
			t.Fatalf("node %d delay assignment is not deterministic", i)
		}
	}
	if len(means) == 1 {
		t.Fatal("all generated nodes received the same assigned delay")
	}
}

func TestDomainDelayDistributionAssignsPerEdgeMeans(t *testing.T) {
	model := DomainTopologyConfig{Model: "path"}
	first := domainScenario(model, 12)
	first.Domain.Edge.DelayDistribution = Distribution{Type: "exponential", MeanMS: 30}
	first.Domain.Edge.DelayJitterStdDevMS = 4
	second := domainScenario(model, 12)
	second.Domain.Edge.DelayDistribution = Distribution{Type: "exponential", MeanMS: 30}
	second.Domain.Edge.DelayJitterStdDevMS = 4

	resolveDomainForTest(t, first)
	resolveDomainForTest(t, second)

	means := make(map[float64]struct{})
	for i, edge := range first.Topology.Edges {
		got := edge.Network.DelayDistribution
		if got.Type != "constant" {
			t.Fatalf("edge %d delay=%+v; want assigned constant baseline", i, got)
		}
		if got.ValueMS < 0 {
			t.Fatalf("edge %d has negative assigned delay %f", i, got.ValueMS)
		}
		if edge.Network.DelayJitterStdDevMS != 4 {
			t.Fatalf("edge %d delay_jitter_stddev=%v; want 4ms", i, edge.Network.DelayJitterStdDevMS)
		}
		means[got.ValueMS] = struct{}{}
		if got != second.Topology.Edges[i].Network.DelayDistribution {
			t.Fatalf("edge %d delay assignment is not deterministic", i)
		}
	}
	if len(means) == 1 {
		t.Fatal("all generated edges received the same assigned delay")
	}
}

func TestDomainNodeMeanSamplingDoesNotChangeTopology(t *testing.T) {
	model := DomainTopologyConfig{Model: "er", P: floatPointer(0.08), EnsureConnected: true}
	constant := domainScenario(model, 40)
	normal := domainScenario(model, 40)
	normal.Domain.Node.ProcessingDelayDistribution = Distribution{Type: "normal", MeanMS: 100, StdDevMS: 25}

	resolveDomainForTest(t, constant)
	resolveDomainForTest(t, normal)
	if !reflect.DeepEqual(constant.Topology.Edges, normal.Topology.Edges) {
		t.Fatal("node performance sampling changed the generated topology")
	}
}

func TestERAverageDegreeMatchesEquivalentProbability(t *testing.T) {
	const n = 200
	const averageDegree = 12.0
	withAverageDegree := domainScenario(DomainTopologyConfig{
		Model: "er", AverageDegree: floatPointer(averageDegree),
	}, n)
	withProbability := domainScenario(DomainTopologyConfig{
		Model: "er", P: floatPointer(averageDegree / float64(n-1)),
	}, n)
	resolveDomainForTest(t, withAverageDegree)
	resolveDomainForTest(t, withProbability)
	if !reflect.DeepEqual(withAverageDegree.Topology.Edges, withProbability.Topology.Edges) {
		t.Fatal("average_degree did not produce the same graph as its equivalent p")
	}
}

func TestERRejectsProbabilityAndAverageDegreeTogether(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{
		Model: "er", P: floatPointer(0.1), AverageDegree: floatPointer(12),
	}, 200)
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err == nil {
		t.Fatal("expected p and average_degree conflict")
	}
}

func TestBAOpportunisticDomainEdgeCountAndDeterminism(t *testing.T) {
	model := DomainTopologyConfig{Model: "ba-opportunistic", M: 3}
	first := domainScenario(model, 80)
	second := domainScenario(model, 80)
	first.Domain.Node.ProcessingDelayDistribution = Distribution{Type: "normal", MeanMS: 50, StdDevMS: 25}
	second.Domain.Node.ProcessingDelayDistribution = Distribution{Type: "normal", MeanMS: 50, StdDevMS: 25}
	first.Domain.Edge.DelayDistribution = Distribution{Type: "exponential", MeanMS: 100}
	second.Domain.Edge.DelayDistribution = Distribution{Type: "exponential", MeanMS: 100}

	resolveDomainForTest(t, first)
	resolveDomainForTest(t, second)

	want := 3*4/2 + (80-4)*3
	if len(first.Topology.Edges) != want {
		t.Fatalf("BA-opportunistic edge count=%d; want %d", len(first.Topology.Edges), want)
	}
	if !reflect.DeepEqual(first.Topology, second.Topology) {
		t.Fatal("same seed and delay distributions generated different BA-opportunistic topology")
	}
}

func TestBAOpportunisticFavorsLowProcessingDelayHub(t *testing.T) {
	const (
		n = 100
		m = 2
	)
	nodes := make([]NodeSpec, n)
	for i := range nodes {
		processingMS := 1000.0
		if i == 0 {
			processingMS = 1
		}
		nodes[i] = NodeSpec{
			ID: "n",
			Performance: &NodePerformance{
				ProcessingDelayDistribution: constantDistribution(processingMS),
				Workers:                     1, QueueCapacity: 16, OverflowPolicy: "drop_new",
			},
		}
	}
	edge := EdgeNetwork{
		DelayDistribution: Distribution{Type: "constant", ValueMS: 50},
		QueueCapacity:     16,
	}

	edges, err := generateBAOpportunistic(n, m, nodes, edge, 42)
	if err != nil {
		t.Fatal(err)
	}
	degrees := make([]int, n)
	for _, edge := range edges {
		degrees[edge.a]++
		degrees[edge.b]++
	}
	if degrees[0] <= degrees[1] || degrees[0] <= degrees[2] {
		t.Fatalf("fast initial node was not favored: degree[0]=%d degree[1]=%d degree[2]=%d", degrees[0], degrees[1], degrees[2])
	}
	if degrees[0] < 30 {
		t.Fatalf("fast node did not become a strong hub: degree[0]=%d", degrees[0])
	}
}

func TestBAOpportunisticRetainsSelectedLowDelayEdges(t *testing.T) {
	const (
		n = 200
		m = 3
	)
	nodes := make([]NodeSpec, n)
	for i := range nodes {
		nodes[i] = NodeSpec{
			ID: "n",
			Performance: &NodePerformance{
				ProcessingDelayDistribution: constantDistribution(0),
				Workers:                     1, QueueCapacity: 16, OverflowPolicy: "drop_new",
			},
		}
	}
	edge := EdgeNetwork{
		DelayDistribution: Distribution{Type: "exponential", MeanMS: 100},
		QueueCapacity:     16,
	}

	edges, err := generateBAOpportunistic(n, m, nodes, edge, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("no edges generated")
	}

	total := 0.0
	count := 0
	for _, generated := range edges {
		if !generated.hasDelayBaseline {
			t.Fatal("BA-opportunistic edge is missing its selected delay baseline")
		}
		total += generated.delayBaselineMS
		count++
	}
	mean := total / float64(count)
	if mean >= 100 {
		t.Fatalf("selected edge mean=%fms; expected opportunistic selection below source mean 100ms", mean)
	}
}

func TestBAOpportunisticInitialCliqueUsesProcessingAndLinkDelay(t *testing.T) {
	processing := []float64{1, 50, 5, 5}
	delays := map[[2]int]float64{
		{0, 1}: 100,
		{0, 2}: 1,
		{0, 3}: 50,
		{1, 2}: 100,
		{1, 3}: 100,
		{2, 3}: 1,
	}
	pairDelay := func(a, b int) float64 {
		if a > b {
			a, b = b, a
		}
		return delays[[2]int{a, b}]
	}

	got := selectOpportunisticInitialClique(4, 3, processing, pairDelay)
	want := []int{0, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("initial clique=%v; want %v", got, want)
	}
}
