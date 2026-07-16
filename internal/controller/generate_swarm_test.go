package controller

import (
	"reflect"
	"testing"

	"github.com/k-p2plab/peerkit/internal/config"
)

func TestSwarmNetworkDefinitionWithoutSubnet(t *testing.T) {
	got := swarmNetworkDefinition(config.SwarmConfig{})
	want := map[string]any{"driver": "overlay", "attachable": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("network = %#v, want %#v", got, want)
	}
}

func TestSwarmNetworkDefinitionWithSubnet(t *testing.T) {
	got := swarmNetworkDefinition(config.SwarmConfig{Network: config.SwarmNetworkConfig{
		Subnet:  "10.200.0.0/16",
		Gateway: "10.200.0.1",
	}})
	ipam, ok := got["ipam"].(map[string]any)
	if !ok {
		t.Fatalf("missing ipam: %#v", got)
	}
	configs := ipam["config"].([]any)
	entry := configs[0].(map[string]any)
	if entry["subnet"] != "10.200.0.0/16" || entry["gateway"] != "10.200.0.1" {
		t.Fatalf("unexpected ipam entry: %#v", entry)
	}
}

func TestSwarmPeerEnvironmentCarriesOverlayCIDR(t *testing.T) {
	scenario := &config.Scenario{Deployment: config.DeploymentConfig{Swarm: config.SwarmConfig{
		Network: config.SwarmNetworkConfig{Subnet: "10.200.0.0/16"},
	}}}
	got := swarmPeerEnvironment(scenario)
	if got["PEERKIT_TASK_SLOT"] != "{{.Task.Slot}}" {
		t.Fatalf("missing task slot: %#v", got)
	}
	if got["PEERKIT_OVERLAY_CIDR"] != "10.200.0.0/16" {
		t.Fatalf("missing overlay CIDR: %#v", got)
	}
}
