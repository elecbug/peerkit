package config

import (
	"testing"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func TestScenarioProtocolDefaultsAndValidation(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	resolveDomainForTest(t, scenario)
	if scenario.Protocol != protocols.BaseFlooding {
		t.Fatalf("default protocol=%q; want %q", scenario.Protocol, protocols.BaseFlooding)
	}

	scenario = domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = protocols.IDontWantFlooding
	resolveDomainForTest(t, scenario)
	if scenario.Protocol != protocols.IDontWantFlooding {
		t.Fatalf("resolved protocol=%q", scenario.Protocol)
	}

	scenario = domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = "invalid"
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err != nil {
		t.Fatal(err)
	}
	scenario.ApplyDefaults()
	if err := scenario.Validate(); err == nil {
		t.Fatal("invalid protocol passed validation")
	}
}
