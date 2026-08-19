package protocols

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeDefaultsToBaseFlooding(t *testing.T) {
	if got := Normalize(""); got != BaseFlooding {
		t.Fatalf("Normalize(empty)=%q; want %q", got, BaseFlooding)
	}
}

func TestValidateSupportedProtocols(t *testing.T) {
	for _, value := range []string{BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding, HopWave} {
		if err := Validate(value); err != nil {
			t.Fatalf("Validate(%q): %v", value, err)
		}
	}
	if err := Validate("unknown"); err == nil {
		t.Fatal("Validate(unknown) succeeded")
	}
}

func TestConfigYAMLLegacyScalarCompatibility(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("idontwant_flooding\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ProtocolName() != IDontWantFlooding || cfg.HopWave != nil {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "idontwant_flooding\n" {
		t.Fatalf("legacy protocol encoded as %q", encoded)
	}
}

func TestConfigYAMLHopWave(t *testing.T) {
	input := []byte("hopwave:\n  i: 3\n  f: 0.5\n")
	var cfg Config
	if err := yaml.Unmarshal(input, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.IsHopWave() || cfg.HopWave == nil || cfg.HopWave.I != 3 || cfg.HopWave.F != 0.5 {
		t.Fatalf("unexpected hopwave config: %#v", cfg)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Config
	if err := yaml.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !roundTrip.IsHopWave() || roundTrip.HopWave.I != 3 || roundTrip.HopWave.F != 0.5 {
		t.Fatalf("round trip mismatch: %#v", roundTrip)
	}
}

func TestConfigJSONSupportsLegacyAndHopWave(t *testing.T) {
	legacy := New(BaseFlooding)
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"base_flooding"` {
		t.Fatalf("legacy JSON=%s", data)
	}

	hopwave := Config{Name: HopWave, HopWave: &HopWaveConfig{I: 2, F: 0.7}}
	data, err = json.Marshal(hopwave)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.IsHopWave() || decoded.HopWave.I != 2 || decoded.HopWave.F != 0.7 {
		t.Fatalf("decoded=%#v", decoded)
	}
}

func TestValidateHopWaveConfig(t *testing.T) {
	valid := Config{Name: HopWave, HopWave: &HopWaveConfig{I: 3, F: 0.5}}
	if err := ValidateConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{
		{Name: HopWave},
		{Name: HopWave, HopWave: &HopWaveConfig{I: 0, F: 0.5}},
		{Name: HopWave, HopWave: &HopWaveConfig{I: 2, F: 0}},
		{Name: HopWave, HopWave: &HopWaveConfig{I: 2, F: 1.1}},
	} {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("invalid config passed: %#v", cfg)
		}
	}
}
