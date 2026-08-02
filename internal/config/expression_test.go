package config

import (
	"math/rand"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseDistributionExpressions(t *testing.T) {
	tests := []struct {
		expression string
		want       Distribution
	}{
		{"100ms", Distribution{Type: "constant", ValueMS: 100}},
		{"constant(1.5s)", Distribution{Type: "constant", ValueMS: 1500}},
		{"uniform(10ms, 50ms)", Distribution{Type: "uniform", MinMS: 10, MaxMS: 50}},
		{"normal(mean=100ms, stddev=20ms)", Distribution{Type: "normal", MeanMS: 100, StdDevMS: 20}},
		{"gaussian(mu=80, sigma=5)", Distribution{Type: "normal", MeanMS: 80, StdDevMS: 5}},
		{"Normal(μ=90ms, σ=15ms)", Distribution{Type: "normal", MeanMS: 90, StdDevMS: 15}},
		{"exponential(25ms)", Distribution{Type: "exponential", MeanMS: 25}},
		{"pareto(scale=20ms, shape=2.5)", Distribution{Type: "pareto", ScaleMS: 20, Shape: 2.5}},
	}

	for _, test := range tests {
		got, err := ParseDistributionExpression(test.expression)
		if err != nil {
			t.Fatalf("ParseDistributionExpression(%q): %v", test.expression, err)
		}
		if got != test.want {
			t.Fatalf("ParseDistributionExpression(%q) = %+v; want %+v", test.expression, got, test.want)
		}
	}
}

func TestDistributionYAMLAcceptsExpressionAndMapping(t *testing.T) {
	var expression Distribution
	if err := yaml.Unmarshal([]byte(`normal(100ms, 20ms)`), &expression); err != nil {
		t.Fatal(err)
	}
	if expression.Type != "normal" || expression.MeanMS != 100 || expression.StdDevMS != 20 {
		t.Fatalf("unexpected expression distribution: %+v", expression)
	}

	var mapping Distribution
	if err := yaml.Unmarshal([]byte("distribution: exponential\nmean_ms: 30\n"), &mapping); err != nil {
		t.Fatal(err)
	}
	if mapping.Type != "exponential" || mapping.MeanMS != 30 {
		t.Fatalf("unexpected mapping distribution: %+v", mapping)
	}
}

func TestMillisecondsYAMLAcceptsDurationAndNumber(t *testing.T) {
	var duration Milliseconds
	if err := duration.UnmarshalYAML(&yaml.Node{Kind: yaml.ScalarNode, Value: "25ms"}); err != nil {
		t.Fatal(err)
	}
	if duration != 25 {
		t.Fatalf("duration=%v; want 25ms", duration)
	}

	var numeric Milliseconds
	if err := numeric.UnmarshalYAML(&yaml.Node{Kind: yaml.ScalarNode, Value: "12.5"}); err != nil {
		t.Fatal(err)
	}
	if numeric != 12.5 {
		t.Fatalf("numeric=%v; want 12.5ms", numeric)
	}
}

func TestNormalDistributionResamplesNegativeDraw(t *testing.T) {
	// Seed 1 produces a first standard-normal draw below -1. With mean=10ms
	// and stddev=10ms, that draw is negative and must be rejected.
	rng := rand.New(rand.NewSource(1))
	d := Distribution{Type: "normal", MeanMS: 10, StdDevMS: 10}
	got := d.SampleMilliseconds(rng)
	if got <= 0 || got >= 10 {
		t.Fatalf("expected a positive resampled value below 10ms, got %v", got)
	}
}
