package main

import (
	"fmt"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
)

func TestRouteStoreScopeCheckErrorsOnCityBeadRoutedToRig(t *testing.T) {
	cityDir := t.TempDir()
	rigDir := t.TempDir()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: rigDir}},
	}
	cityStore := beads.NewMemStoreFrom(0, []beads.Bead{
		{ID: "gc-1", Title: "wrong store", Status: "open", Type: "task", Metadata: map[string]string{"gc.routed_to": "repo/worker"}},
	}, nil)
	rigStore := beads.NewMemStore()

	result := newRouteStoreScopeCheck(cfg, cityDir, routeStoreTestFactory(map[string]beads.Store{
		cityDir: cityStore,
		rigDir:  rigStore,
	})).Run(&doctor.CheckContext{CityPath: cityDir})

	if result.Status != doctor.StatusError {
		t.Fatalf("status = %v, want error: %#v", result.Status, result)
	}
	if len(result.Details) != 1 || result.Details[0] == "" {
		t.Fatalf("details = %#v, want one mismatch", result.Details)
	}
}

func TestRouteStoreScopeCheckFixMigratesAndClosesSource(t *testing.T) {
	cityDir := t.TempDir()
	rigDir := t.TempDir()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: rigDir}},
	}
	cityStore := beads.NewMemStoreFrom(0, []beads.Bead{
		{
			ID:          "gc-1",
			Title:       "wrong store",
			Status:      "open",
			Type:        "task",
			Description: "do the work",
			Metadata:    map[string]string{"gc.routed_to": "repo/worker"},
		},
	}, nil)
	rigStore := beads.NewMemStore()
	check := newRouteStoreScopeCheck(cfg, cityDir, routeStoreTestFactory(map[string]beads.Store{
		cityDir: cityStore,
		rigDir:  rigStore,
	}))

	if err := check.Fix(&doctor.CheckContext{CityPath: cityDir}); err != nil {
		t.Fatalf("Fix: %v", err)
	}

	closed, err := cityStore.Get("gc-1")
	if err != nil {
		t.Fatalf("source Get: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("source status = %q, want closed", closed.Status)
	}
	if closed.Metadata["gc.routed_to"] != "" {
		t.Fatalf("source gc.routed_to = %q, want cleared", closed.Metadata["gc.routed_to"])
	}

	migrated, err := rigStore.List(beads.ListQuery{AllowScan: true})
	if err != nil {
		t.Fatalf("rig List: %v", err)
	}
	if len(migrated) != 1 {
		t.Fatalf("migrated = %#v, want one bead", migrated)
	}
	if migrated[0].Title != "wrong store" || migrated[0].Description != "do the work" {
		t.Fatalf("migrated bead did not preserve title/description: %#v", migrated[0])
	}
	if migrated[0].Metadata["gc.routed_to"] != "repo/worker" {
		t.Fatalf("migrated gc.routed_to = %q, want repo/worker", migrated[0].Metadata["gc.routed_to"])
	}
	if migrated[0].Metadata["gc.migrated_from_bead"] != "gc-1" {
		t.Fatalf("migrated_from_bead = %q, want gc-1", migrated[0].Metadata["gc.migrated_from_bead"])
	}
}

func TestRouteStoreScopeCheckFixMigratesRigInfraBeadToCity(t *testing.T) {
	cityDir := t.TempDir()
	rigDir := t.TempDir()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Rigs:      []config.Rig{{Name: "repo", Path: rigDir}},
		Agents:    []config.Agent{{Name: "infra-worker", Scope: "city"}},
	}
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStoreFrom(0, []beads.Bead{
		{
			ID:          "rb-1",
			Title:       "infra smell",
			Status:      "open",
			Type:        "task",
			Description: "fix city runtime",
			Metadata:    map[string]string{"gc.routed_to": "infra-worker"},
		},
	}, nil)
	check := newRouteStoreScopeCheck(cfg, cityDir, routeStoreTestFactory(map[string]beads.Store{
		cityDir: cityStore,
		rigDir:  rigStore,
	}))

	if err := check.Fix(&doctor.CheckContext{CityPath: cityDir}); err != nil {
		t.Fatalf("Fix: %v", err)
	}

	closed, err := rigStore.Get("rb-1")
	if err != nil {
		t.Fatalf("source Get: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("source status = %q, want closed", closed.Status)
	}
	if closed.Metadata["gc.canonical_store"] != "city:test-city" {
		t.Fatalf("source canonical store = %q, want city:test-city", closed.Metadata["gc.canonical_store"])
	}

	migrated, err := cityStore.List(beads.ListQuery{AllowScan: true})
	if err != nil {
		t.Fatalf("city List: %v", err)
	}
	if len(migrated) != 1 {
		t.Fatalf("migrated = %#v, want one bead", migrated)
	}
	if migrated[0].Metadata["gc.routed_to"] != "infra-worker" {
		t.Fatalf("migrated gc.routed_to = %q, want infra-worker", migrated[0].Metadata["gc.routed_to"])
	}
	if migrated[0].Metadata["gc.migrated_from_store"] != "rig:repo" {
		t.Fatalf("migrated_from_store = %q, want rig:repo", migrated[0].Metadata["gc.migrated_from_store"])
	}
}

func routeStoreTestFactory(stores map[string]beads.Store) func(string) (beads.Store, error) {
	return func(path string) (beads.Store, error) {
		store, ok := stores[path]
		if !ok {
			return nil, fmt.Errorf("unexpected store path %q", path)
		}
		return store, nil
	}
}
