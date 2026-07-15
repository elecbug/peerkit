package metrics

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/k-p2plab/peerkit/internal/config"
)

func TestWriterFlushesQueuedEventsOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := NewWriter(path, config.MetricsConfig{
		BufferBytes: 4096, QueueCapacity: 8, FlushIntervalMS: 60_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := writer.Write(Event{Type: "test", Sequence: uint64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 100 {
		t.Fatalf("lines=%d; want 100", lines)
	}
}
