package config

import "testing"

func TestDeploymentDefaultsToCompose(t *testing.T) {
	scenario := Scenario{}
	scenario.ApplyDefaults()
	if scenario.Deployment.Mode != "compose" {
		t.Fatalf("expected compose mode, got %q", scenario.Deployment.Mode)
	}
	if scenario.Deployment.ComposeParallelism != 4 {
		t.Fatalf("expected compose parallelism 4, got %d", scenario.Deployment.ComposeParallelism)
	}
}

func TestSwarmDefaults(t *testing.T) {
	scenario := Scenario{Deployment: DeploymentConfig{Mode: "SWARM"}}
	scenario.ApplyDefaults()
	if !scenario.Deployment.IsSwarm() {
		t.Fatalf("expected normalized swarm mode, got %q", scenario.Deployment.Mode)
	}
	if !scenario.Deployment.Swarm.PushImageEnabled() {
		t.Fatal("expected push_image default true")
	}
	if !scenario.Deployment.Swarm.WithRegistryAuthEnabled() {
		t.Fatal("expected with_registry_auth default true")
	}
	if scenario.Deployment.Swarm.StartupTimeoutSeconds != 600 {
		t.Fatalf("unexpected startup timeout %d", scenario.Deployment.Swarm.StartupTimeoutSeconds)
	}
	if scenario.Deployment.Swarm.StartupBatchSize != 25 {
		t.Fatalf("unexpected startup batch size %d", scenario.Deployment.Swarm.StartupBatchSize)
	}
	if scenario.Deployment.Swarm.StartupBatchIntervalMS != 1000 {
		t.Fatalf("unexpected startup batch interval %d", scenario.Deployment.Swarm.StartupBatchIntervalMS)
	}
}

func TestSwarmRejectsHeterogeneousContainerLimits(t *testing.T) {
	nodes := []NodeSpec{
		{ID: "n0", Resources: &ResourceConfig{CPULimit: 0.25, MemoryLimitMB: 128}},
		{ID: "n1", Resources: &ResourceConfig{CPULimit: 0.50, MemoryLimitMB: 128}},
	}
	if err := validateUniformSwarmResources(nodes); err == nil {
		t.Fatal("expected heterogeneous resource limits to fail")
	}
}
