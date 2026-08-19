package config

import (
	"testing"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func TestScenarioProtocolDefaultsAndValidation(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	resolveDomainForTest(t, scenario)
	if scenario.Protocol.ProtocolName() != protocols.BaseFlooding {
		t.Fatalf("default protocol=%q; want %q", scenario.Protocol.String(), protocols.BaseFlooding)
	}

	scenario = domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = protocols.New(protocols.IDontWantFlooding)
	resolveDomainForTest(t, scenario)
	if scenario.Protocol.ProtocolName() != protocols.IDontWantFlooding {
		t.Fatalf("resolved protocol=%q", scenario.Protocol.String())
	}

	scenario = domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = protocols.New("invalid")
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err != nil {
		t.Fatal(err)
	}
	scenario.ApplyDefaults()
	if err := scenario.Validate(); err == nil {
		t.Fatal("invalid protocol passed validation")
	}
}

func TestScenarioHopWaveProtocolValidation(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = protocols.Config{
		Name:    protocols.HopWave,
		HopWave: &protocols.HopWaveConfig{I: 3, F: 0.5},
	}
	resolveDomainForTest(t, scenario)
	if !scenario.Protocol.IsHopWave() || scenario.Protocol.HopWave.I != 3 || scenario.Protocol.HopWave.F != 0.5 {
		t.Fatalf("resolved protocol=%#v", scenario.Protocol)
	}
}
