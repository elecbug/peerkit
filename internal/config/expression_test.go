package config

import (
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
