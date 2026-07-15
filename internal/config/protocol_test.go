package config

import (
	"testing"

	"github.com/k-p2plab/peerkit/internal/protocols"
)

func TestProtocolDefaultsToBaseFlooding(t *testing.T) {
	scenario := Scenario{}
	scenario.ApplyDefaults()
	if scenario.Protocol != protocols.BaseFlooding {
		t.Fatalf("protocol=%q; want %q", scenario.Protocol, protocols.BaseFlooding)
	}
}

func TestProtocolValidation(t *testing.T) {
	for _, protocolName := range []string{protocols.BaseFlooding, protocols.DuplicateAwareFlooding} {
		scenario := domainScenario(DomainTopologyConfig{Model: "path"}, 3)
		scenario.Protocol = protocolName
		resolveDomainForTest(t, scenario)
	}
}

func TestProtocolValidationRejectsUnknown(t *testing.T) {
	scenario := domainScenario(DomainTopologyConfig{Model: "path"}, 3)
	scenario.Protocol = "unknown"
	scenario.ApplyDefaults()
	if err := scenario.ExpandDomain(); err != nil {
		t.Fatal(err)
	}
	scenario.ApplyDefaults()
	if err := scenario.Validate(); err == nil {
		t.Fatal("expected unsupported protocol error")
	}
}
