package sling

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

func TestCheckRouteStoreScopeRejectsCityStoreForRigTarget(t *testing.T) {
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: "/repo"}},
		Agents:    []config.Agent{{Name: "worker", Dir: "repo"}},
	}

	err := CheckRouteStoreScope("repo/worker", "city:test-city", "test-city", cfg)
	if err == nil {
		t.Fatal("CheckRouteStoreScope error = nil, want mismatch")
	}
	if !strings.Contains(err.Error(), "belongs in rig:repo") {
		t.Fatalf("error = %q, want rig:repo guidance", err.Error())
	}
}

func TestCheckRouteStoreScopeRejectsRigStoreForCityTarget(t *testing.T) {
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: "/repo"}},
		Agents:    []config.Agent{{Name: "infra-worker", Scope: "city"}},
	}

	err := CheckRouteStoreScope("infra-worker", "rig:repo", "test-city", cfg)
	if err == nil {
		t.Fatal("CheckRouteStoreScope error = nil, want mismatch")
	}
	if !strings.Contains(err.Error(), "belongs in city:test-city") {
		t.Fatalf("error = %q, want city guidance", err.Error())
	}
}

func TestCheckRouteStoreScopeAllowsMatchingRigTarget(t *testing.T) {
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: "/repo"}},
		Agents:    []config.Agent{{Name: "worker", Dir: "repo"}},
	}

	if err := CheckRouteStoreScope("repo/worker", "rig:repo", "test-city", cfg); err != nil {
		t.Fatalf("CheckRouteStoreScope() = %v, want nil", err)
	}
}
