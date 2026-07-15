package model

import (
	"math/rand"
	"testing"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func TestConstantDuration(t *testing.T) {
	got := SampleDuration(config.Distribution{Type: "constant", ValueMS: 25}, rand.New(rand.NewSource(1)))
	if got != 25*time.Millisecond {
		t.Fatalf("expected 25ms, got %v", got)
	}
}

func TestSerializationDelay(t *testing.T) {
	got := SerializationDelay(1_000_000, 8)
	if got != time.Second {
		t.Fatalf("expected 1s, got %v", got)
	}
}
