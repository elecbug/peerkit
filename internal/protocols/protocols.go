package protocols

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	BaseFlooding           = "base_flooding"
	DuplicateAwareFlooding = "duplicate_aware_flooding"
	IDontWantFlooding      = "idontwant_flooding"
	HopWave                = "hopwave"
)

// HopWaveConfig controls HopWave's periodic propagation reinforcement.
// I is the number of relay levels in one Crest-to-Crest cycle and F is the
// fraction of eligible neighbors selected by each Trough Relay.
type HopWaveConfig struct {
	I int     `yaml:"i" json:"i"`
	F float64 `yaml:"f" json:"f"`
}

// Config is a backwards-compatible protocol configuration. Legacy protocols
// remain scalar strings, while HopWave is represented as:
//
//	protocol:
//	  hopwave:
//	    i: 3
//	    f: 0.5
//
// Keeping the union in one type lets existing scenarios continue to use
// `protocol: base_flooding`, `duplicate_aware_flooding`, or
// `idontwant_flooding` unchanged.
type Config struct {
	Name    string
	HopWave *HopWaveConfig
}

func New(value string) Config {
	return NormalizeConfig(Config{Name: value})
}

func (c Config) String() string {
	name := Normalize(c.Name)
	if name != HopWave || c.HopWave == nil {
		return name
	}
	return fmt.Sprintf("%s(i=%d,f=%g)", HopWave, c.HopWave.I, c.HopWave.F)
}

func (c Config) ProtocolName() string {
	return Normalize(c.Name)
}

func (c Config) IsHopWave() bool {
	return c.ProtocolName() == HopWave
}

func NormalizeConfig(c Config) Config {
	c.Name = Normalize(c.Name)
	if c.Name != HopWave {
		c.HopWave = nil
	}
	return c
}

func ValidateConfig(c Config) error {
	c = NormalizeConfig(c)
	if err := Validate(c.Name); err != nil {
		return err
	}
	if c.Name != HopWave {
		return nil
	}
	if c.HopWave == nil {
		return fmt.Errorf("hopwave protocol requires protocol.hopwave.i and protocol.hopwave.f")
	}
	if c.HopWave.I < 1 {
		return fmt.Errorf("protocol.hopwave.i must be at least 1")
	}
	if c.HopWave.F <= 0 || c.HopWave.F > 1 {
		return fmt.Errorf("protocol.hopwave.f must be greater than 0 and at most 1")
	}
	return nil
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		*c = New("")
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" && node.Tag != "" {
			return fmt.Errorf("protocol must be a string or a hopwave mapping")
		}
		*c = New(node.Value)
		return nil
	case yaml.MappingNode:
		var wrapper struct {
			HopWave *HopWaveConfig `yaml:"hopwave"`
		}
		if err := node.Decode(&wrapper); err != nil {
			return fmt.Errorf("decode protocol: %w", err)
		}
		if wrapper.HopWave == nil {
			return fmt.Errorf("protocol mapping must contain hopwave")
		}
		if len(node.Content) != 2 || node.Content[0].Value != HopWave {
			return fmt.Errorf("protocol mapping supports only hopwave")
		}
		*c = Config{Name: HopWave, HopWave: wrapper.HopWave}
		return nil
	default:
		return fmt.Errorf("protocol must be a string or a hopwave mapping")
	}
}

func (c Config) MarshalYAML() (any, error) {
	c = NormalizeConfig(c)
	if c.Name != HopWave {
		return c.Name, nil
	}
	return struct {
		HopWave *HopWaveConfig `yaml:"hopwave"`
	}{HopWave: c.HopWave}, nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("empty protocol configuration")
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*c = New(value)
		return nil
	}
	var wrapper struct {
		HopWave *HopWaveConfig `json:"hopwave"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return fmt.Errorf("decode protocol: %w", err)
	}
	if wrapper.HopWave == nil {
		return fmt.Errorf("protocol object must contain hopwave")
	}
	*c = Config{Name: HopWave, HopWave: wrapper.HopWave}
	return nil
}

func (c Config) MarshalJSON() ([]byte, error) {
	c = NormalizeConfig(c)
	if c.Name != HopWave {
		return json.Marshal(c.Name)
	}
	return json.Marshal(struct {
		HopWave *HopWaveConfig `json:"hopwave"`
	}{HopWave: c.HopWave})
}

func Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return BaseFlooding
	}
	return value
}

func Validate(value string) error {
	switch Normalize(value) {
	case BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding, HopWave:
		return nil
	default:
		return fmt.Errorf("unsupported protocol %q; supported protocols are %s, %s, %s, and %s",
			value, BaseFlooding, DuplicateAwareFlooding, IDontWantFlooding, HopWave)
	}
}

func UsesDuplicateNeighborSuppression(value string) bool {
	return Normalize(value) == DuplicateAwareFlooding
}

func UsesIDontWant(value string) bool {
	return Normalize(value) == IDontWantFlooding
}
