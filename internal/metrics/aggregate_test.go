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
		{TimestampNS: 1_000_000, Type: "message_created", Node: "n0", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 2_000_000, Type: "message_sent", Node: "n0", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 4_000_000, Type: "message_received", Node: "n1", MessageID: "m1", Origin: "n0"},
		{TimestampNS: 5_000_000, Type: "message_received", Node: "n1", MessageID: "m1", Origin: "n0", Duplicate: true},
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
	if summary.Messages != 1 || summary.AverageReachability != 1 || summary.TotalDuplicates != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
