package model

import (
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

func SampleDuration(d config.Distribution, rng *rand.Rand) time.Duration {
	var milliseconds float64
	switch strings.ToLower(d.Type) {
	case "constant":
		milliseconds = d.ValueMS
	case "uniform":
		milliseconds = d.MinMS + rng.Float64()*(d.MaxMS-d.MinMS)
	case "normal":
		milliseconds = d.MeanMS + rng.NormFloat64()*d.StdDevMS
	case "exponential":
		if d.MeanMS > 0 {
			milliseconds = rng.ExpFloat64() * d.MeanMS
		}
	case "pareto":
		u := rng.Float64()
		milliseconds = d.ScaleMS / math.Pow(1-u, 1/d.Shape)
	}
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
