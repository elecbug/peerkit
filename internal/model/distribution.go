package model

import (
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func SampleDuration(d config.Distribution, rng *rand.Rand) time.Duration {
	return millisecondsToDuration(d.SampleMilliseconds(rng))
}

// SampleAroundAssignedDelay draws a runtime delay from a zero-truncated normal
// distribution centered on a previously assigned baseline delay. Resolved
// scenarios encode the assigned baseline as a constant distribution.
//
// To keep jitter a small local variation rather than a replacement for the
// assigned baseline, the effective standard deviation is capped at one third
// of the baseline. This also keeps the rejected negative tail below roughly
// 0.14% whenever the configured standard deviation reaches the cap.
func SampleAroundAssignedDelay(assigned config.Distribution, configuredJitterStdDevMS config.Milliseconds, rng *rand.Rand) time.Duration {
	meanMS := assigned.ValueMS
	if !strings.EqualFold(assigned.Type, "constant") {
		// Runtime configs are expected to be resolved before execution. Falling
		// back to one draw keeps manually constructed configs usable without
		// changing the behavior of resolved scenarios.
		meanMS = assigned.SampleMilliseconds(rng)
	}
	stdDevMS := effectiveJitterStdDev(meanMS, float64(configuredJitterStdDevMS))
	milliseconds := sampleNonNegativeNormal(meanMS, stdDevMS, rng)
	return millisecondsToDuration(milliseconds)
}

func effectiveJitterStdDev(meanMS, configuredStdDevMS float64) float64 {
	if meanMS <= 0 || configuredStdDevMS <= 0 || math.IsNaN(meanMS) || math.IsNaN(configuredStdDevMS) || math.IsInf(meanMS, 0) || math.IsInf(configuredStdDevMS, 0) {
		return 0
	}
	maximum := meanMS / 3
	if configuredStdDevMS > maximum {
		return maximum
	}
	return configuredStdDevMS
}

func sampleNonNegativeNormal(meanMS, stdDevMS float64, rng *rand.Rand) float64 {
	if meanMS <= 0 {
		return 0
	}
	if stdDevMS <= 0 {
		return meanMS
	}
	for {
		sample := meanMS + rng.NormFloat64()*stdDevMS
		if sample >= 0 && !math.IsNaN(sample) && !math.IsInf(sample, 0) {
			return sample
		}
	}
}

func millisecondsToDuration(milliseconds float64) time.Duration {
	if milliseconds < 0 || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) {
		milliseconds = 0
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

func SerializationDelay(payloadBytes int, bandwidthMbps float64) time.Duration {
	if payloadBytes <= 0 || bandwidthMbps <= 0 {
		return 0
	}
	seconds := (float64(payloadBytes) * 8) / (bandwidthMbps * 1_000_000)
	return time.Duration(seconds * float64(time.Second))
}
