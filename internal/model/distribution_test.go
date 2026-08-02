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

func TestSampleAroundAssignedDelayUsesAssignedMean(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	assigned := config.Distribution{Type: "constant", ValueMS: 30}
	if got := SampleAroundAssignedDelay(assigned, 0, rng); got != 30*time.Millisecond {
		t.Fatalf("expected fixed assigned delay 30ms, got %v", got)
	}
}

func TestSampleAroundAssignedDelayCapsJitterForLowBaseline(t *testing.T) {
	if got := effectiveJitterStdDev(3, 5); got != 1 {
		t.Fatalf("effective jitter=%vms; want 1ms", got)
	}
	if got := effectiveJitterStdDev(30, 3); got != 3 {
		t.Fatalf("effective jitter=%vms; want configured 3ms", got)
	}
}

func TestSampleAroundAssignedDelayResamplesNegativeDraw(t *testing.T) {
	// Seed 399 yields a first normal draw below -3 sigma. With mean=30ms and
	// the low-baseline cap, the effective standard deviation is 10ms, so the
	// first draw is negative and must be rejected. The next draw is positive.
	rng := rand.New(rand.NewSource(399))
	assigned := config.Distribution{Type: "constant", ValueMS: 30}
	got := SampleAroundAssignedDelay(assigned, 100, rng)
	if got <= 30*time.Millisecond || got >= 33*time.Millisecond {
		t.Fatalf("expected resampled delay near 32ms, got %v", got)
	}
}

func TestZeroBaselineRemainsZeroWithConfiguredJitter(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	assigned := config.Distribution{Type: "constant", ValueMS: 0}
	if got := SampleAroundAssignedDelay(assigned, 100, rng); got != 0 {
		t.Fatalf("zero baseline must remain zero, got %v", got)
	}
}
