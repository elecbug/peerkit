package config

import (
	"math"
	"math/rand"
	"strings"
)

// SampleMilliseconds draws one non-negative millisecond value from the
// distribution. Normal distributions are sampled as zero-truncated normals:
// negative draws are rejected and resampled instead of being clamped to zero.
func (d Distribution) SampleMilliseconds(rng *rand.Rand) float64 {
	var milliseconds float64
	switch strings.ToLower(d.Type) {
	case "constant":
		milliseconds = d.ValueMS
	case "uniform":
		milliseconds = d.MinMS + rng.Float64()*(d.MaxMS-d.MinMS)
	case "normal":
		milliseconds = sampleNonNegativeNormal(d.MeanMS, d.StdDevMS, rng)
	case "exponential":
		if d.MeanMS > 0 {
			milliseconds = rng.ExpFloat64() * d.MeanMS
		}
	case "pareto":
		u := rng.Float64()
		milliseconds = d.ScaleMS / math.Pow(1-u, 1/d.Shape)
	}
	if milliseconds < 0 || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) {
		return 0
	}
	return milliseconds
}

func sampleNonNegativeNormal(meanMS, stdDevMS float64, rng *rand.Rand) float64 {
	if meanMS < 0 || math.IsNaN(meanMS) || math.IsInf(meanMS, 0) {
		return 0
	}
	if stdDevMS <= 0 || math.IsNaN(stdDevMS) || math.IsInf(stdDevMS, 0) {
		return meanMS
	}
	for {
		sample := meanMS + rng.NormFloat64()*stdDevMS
		if sample >= 0 && !math.IsNaN(sample) && !math.IsInf(sample, 0) {
			return sample
		}
	}
}

func constantDistribution(milliseconds float64) Distribution {
	if milliseconds < 0 || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) {
		milliseconds = 0
	}
	return Distribution{Type: "constant", ValueMS: milliseconds}
}
