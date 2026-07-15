package controller

import (
	"reflect"
	"testing"

	"github.com/k-p2plab/peerkit/internal/config"
)

func TestBuildTrafficPlanRandomIsDeterministic(t *testing.T) {
	scenario := &config.Scenario{
		Experiment: config.ExperimentConfig{Seed: 42},
		Topology: config.TopologyConfig{Nodes: []config.NodeSpec{
			{ID: "n0"}, {ID: "n1"}, {ID: "n2"}, {ID: "n3"},
		}},
		Traffic: []config.TrafficConfig{{
			Source: config.RandomTrafficSource, StartAtMS: 10,
			Count: 20, IntervalMS: 5,
		}},
	}

	first, firstEntries := buildTrafficPlan(scenario)
	second, secondEntries := buildTrafficPlan(scenario)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstEntries, secondEntries) {
		t.Fatal("same seed produced different traffic plans")
	}
	valid := map[string]bool{"n0": true, "n1": true, "n2": true, "n3": true}
	seen := map[string]bool{}
	for i, source := range first[0] {
		if !valid[source] {
			t.Fatalf("message %d selected unknown source %q", i, source)
		}
		seen[source] = true
		if got, want := firstEntries[i].ScheduledMS, int64(10+i*5); got != want {
			t.Fatalf("message %d scheduled at %d, want %d", i, got, want)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("random plan unexpectedly selected only one source: %v", first[0])
	}
}

func TestBuildTrafficPlanFixedSource(t *testing.T) {
	scenario := &config.Scenario{
		Experiment: config.ExperimentConfig{Seed: 7},
		Topology:   config.TopologyConfig{Nodes: []config.NodeSpec{{ID: "n0"}, {ID: "n1"}}},
		Traffic:    []config.TrafficConfig{{Source: "n1", Count: 3, IntervalMS: 10}},
	}
	sources, _ := buildTrafficPlan(scenario)
	want := []string{"n1", "n1", "n1"}
	if !reflect.DeepEqual(sources[0], want) {
		t.Fatalf("fixed sources = %v, want %v", sources[0], want)
	}
}
