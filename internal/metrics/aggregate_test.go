package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAggregate(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "n0.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	events := []Event{
		{TimestampNS: 500_000, Type: "peer_started", Node: "n0", Protocol: "idontwant_flooding"},
		{TimestampNS: 1_000_000, Type: "message_created", Node: "n0", MessageID: "m1", Origin: "n0", Protocol: "idontwant_flooding"},
		{TimestampNS: 2_000_000, Type: "message_sent", Node: "n0", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 4_000_000, Type: "message_received", Node: "n1", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 5_000_000, Type: "message_received", Node: "n1", MessageID: "m1", Origin: "n0", Duplicate: true},
		{TimestampNS: 5_500_000, Type: "message_suppressed", Node: "n0", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 6_000_000, Type: "control_sent", Node: "n0", MessageID: "m1", Origin: "n0", ControlBytes: 64},
		{TimestampNS: 7_000_000, Type: "control_received", Node: "n1", MessageID: "m1", Origin: "n0"},
	}
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	summary, err := Aggregate(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Protocol != "idontwant_flooding" || summary.Messages != 1 || summary.AverageReachability != 1 ||
		summary.TotalDuplicates != 1 || summary.TotalSuppressions != 1 || summary.TotalControlSent != 1 ||
		summary.TotalControlReceived != 1 || summary.TotalControlBytesSent != 64 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
