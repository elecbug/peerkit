package controller

import (
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"

	"github.com/k-p2plab/peerkit/internal/config"
)

type trafficPlanEntry struct {
	TrafficIndex int
	MessageIndex int
	ScheduledMS  int64
	Source       string
}

// buildTrafficPlan resolves every configured emission to a concrete source.
// Fixed sources remain unchanged. The reserved source "random" selects a node
// independently for every message using a deterministic RNG derived from the
// experiment seed and traffic index.
func buildTrafficPlan(scenario *config.Scenario) ([][]string, []trafficPlanEntry) {
	nodeIDs := make([]string, len(scenario.Topology.Nodes))
	for i, node := range scenario.Topology.Nodes {
		nodeIDs[i] = node.ID
	}

	sources := make([][]string, len(scenario.Traffic))
	entries := make([]trafficPlanEntry, 0)
	for trafficIndex, traffic := range scenario.Traffic {
		sources[trafficIndex] = make([]string, traffic.Count)
		var rng *rand.Rand
		if config.IsRandomTrafficSource(traffic.Source) {
			rng = rand.New(rand.NewSource(trafficSeed(scenario.Experiment.Seed, trafficIndex)))
		}
		for messageIndex := 0; messageIndex < traffic.Count; messageIndex++ {
			source := traffic.Source
			if rng != nil {
				source = nodeIDs[rng.Intn(len(nodeIDs))]
			}
			sources[trafficIndex][messageIndex] = source
			entries = append(entries, trafficPlanEntry{
				TrafficIndex: trafficIndex,
				MessageIndex: messageIndex,
				ScheduledMS:  traffic.StartAtMS + int64(messageIndex)*traffic.IntervalMS,
				Source:       source,
			})
		}
	}
	return sources, entries
}

func writeTrafficPlan(runDir string, entries []trafficPlanEntry) error {
	path := filepath.Join(runDir, "traffic-plan.csv")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create traffic plan: %w", err)
	}
	writer := csv.NewWriter(file)
	writeErr := writer.Write([]string{"traffic_index", "message_index", "scheduled_at_ms", "source"})
	if writeErr == nil {
		for _, entry := range entries {
			writeErr = writer.Write([]string{
				strconv.Itoa(entry.TrafficIndex),
				strconv.Itoa(entry.MessageIndex),
				strconv.FormatInt(entry.ScheduledMS, 10),
				entry.Source,
			})
			if writeErr != nil {
				break
			}
		}
	}
	writer.Flush()
	if writeErr == nil {
		writeErr = writer.Error()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write traffic plan: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close traffic plan: %w", closeErr)
	}
	return nil
}

func trafficSeed(base int64, trafficIndex int) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("peerkit-traffic-%d", trafficIndex)))
	return base ^ int64(h.Sum64())
}
