package sling

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
)

// RouteStoreScopeError reports that a route target is being written into a
// bead store the target does not read.
type RouteStoreScopeError struct {
	Target   string
	Actual   string
	Expected string
}

func (e *RouteStoreScopeError) Error() string {
	return fmt.Sprintf("route/store mismatch: target %q belongs in %s, but the source bead is in %s", e.Target, e.Expected, e.Actual)
}

// AgentExpectedStoreRef returns the store ref an agent should read from.
func AgentExpectedStoreRef(a config.Agent, cityName string) string {
	if strings.TrimSpace(a.Scope) == "city" {
		return cityStoreRef(cityName)
	}
	if dir := strings.TrimSpace(a.Dir); dir != "" {
		return "rig:" + dir
	}
	return cityStoreRef(cityName)
}

// NamedSessionExpectedStoreRef returns the store ref a named session should
// read from.
func NamedSessionExpectedStoreRef(s config.NamedSession, cityName string) string {
	if strings.TrimSpace(s.Scope) == "city" {
		return cityStoreRef(cityName)
	}
	if dir := strings.TrimSpace(s.Dir); dir != "" {
		return "rig:" + dir
	}
	return cityStoreRef(cityName)
}

// RouteExpectedStoreRef resolves a routed_to target to the store ref that owns
// claimable work for that target. It returns "" when the target cannot be
// classified from config or the canonical route shape.
func RouteExpectedStoreRef(cfg *config.City, target, cityName string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if cfg != nil {
		for _, a := range cfg.Agents {
			if target == a.QualifiedName() || target == a.Name || target == a.BindingQualifiedName() {
				return AgentExpectedStoreRef(a, cityName)
			}
		}
		for _, s := range cfg.NamedSessions {
			if target == s.QualifiedName() || target == s.IdentityName() || target == s.TemplateQualifiedName() {
				return NamedSessionExpectedStoreRef(s, cityName)
			}
		}
	}
	dir, _ := config.ParseQualifiedName(target)
	if dir == "" {
		return cityStoreRef(cityName)
	}
	if cfg != nil {
		for _, rig := range cfg.Rigs {
			if rig.Name == dir {
				return "rig:" + dir
			}
		}
		return ""
	}
	return "rig:" + dir
}

// CheckRouteStoreScope verifies that a route target is being written in the
// store that target will query for work.
func CheckRouteStoreScope(target, actualStoreRef, cityName string, cfg *config.City) *RouteStoreScopeError {
	expected := RouteExpectedStoreRef(cfg, target, cityName)
	if expected == "" {
		return nil
	}
	actualKind, actualName := splitStoreRef(actualStoreRef)
	expectedKind, expectedName := splitStoreRef(expected)
	if actualKind == "" || expectedKind == "" {
		return nil
	}
	if actualKind == expectedKind && (actualKind == "city" || actualName == expectedName) {
		return nil
	}
	return &RouteStoreScopeError{
		Target:   strings.TrimSpace(target),
		Actual:   normalizeStoreRef(actualStoreRef),
		Expected: expected,
	}
}

func cityStoreRef(cityName string) string {
	cityName = strings.TrimSpace(cityName)
	if cityName == "" {
		cityName = "city"
	}
	return "city:" + cityName
}

func normalizeStoreRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "unknown"
	}
	return ref
}

func splitStoreRef(ref string) (kind, name string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	kind, name, ok := strings.Cut(ref, ":")
	if !ok {
		return "", ""
	}
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if kind == "" {
		return "", ""
	}
	return kind, name
}
