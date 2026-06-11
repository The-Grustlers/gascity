package controlkind

import "testing"

func TestReviewQuorumKindsAreControlDispatcherGraphKinds(t *testing.T) {
	for _, kind := range []string{ReviewQuorumPlan, ReviewQuorumFinalize} {
		if !IsControlDispatcher(kind) {
			t.Fatalf("%s IsControlDispatcher = false, want true", kind)
		}
		if !RequiresGraphContract(kind) {
			t.Fatalf("%s RequiresGraphContract = false, want true", kind)
		}
		if !IsDetachedGraphStep(kind) {
			t.Fatalf("%s IsDetachedGraphStep = false, want true", kind)
		}
		if !IsScopeCheckExempt(kind) {
			t.Fatalf("%s IsScopeCheckExempt = false, want true", kind)
		}
	}
}

func TestControlKindClassificationsPreserveExistingDistinctions(t *testing.T) {
	if !IsControlDispatcher(Retry) {
		t.Fatal("retry should route to control dispatcher")
	}
	if IsScopeCheckExempt(Retry) {
		t.Fatal("retry should not be scope-check exempt")
	}
	if !IsWorkflowTopology(Workflow) || !IsWorkflowTopology(Scope) || !IsWorkflowTopology(Spec) {
		t.Fatal("workflow, scope, and spec should be workflow topology kinds")
	}
	if IsControlDispatcher(Workflow) || IsControlDispatcher(Scope) || IsControlDispatcher(Spec) {
		t.Fatal("workflow topology kinds should not route to control dispatcher")
	}
	if IsControlDispatcher("task") || RequiresGraphContract("task") {
		t.Fatal("ordinary task kind should not have control metadata")
	}
}

func TestControlCatalogRuntimeRequirements(t *testing.T) {
	fragment := RuntimeRequirements{NeedsCityConfig: true, NeedsFormulaSearchPaths: true, NeedsPrepareFragment: true}
	retry := RuntimeRequirements{NeedsCityConfig: true, NeedsFormulaSearchPaths: true, NeedsSessionRecycle: true}
	tests := map[string]RuntimeRequirements{
		Check:            fragment,
		Fanout:           fragment,
		Ralph:            retry,
		Retry:            retry,
		RetryEval:        {NeedsCityConfig: true, NeedsSessionRecycle: true},
		WorkflowFinalize: {NeedsCityConfig: true, NeedsSourceWorkflowCoordination: true},
	}
	for kind, want := range tests {
		got, ok := RuntimeRequirementsFor(kind)
		if !ok {
			t.Fatalf("RuntimeRequirementsFor(%q) not found", kind)
		}
		if got != want {
			t.Fatalf("RuntimeRequirementsFor(%q) = %+v, want %+v", kind, got, want)
		}
	}
	if _, ok := RuntimeRequirementsFor(Workflow); ok {
		t.Fatal("workflow topology kind unexpectedly has control runtime requirements")
	}
}

func TestControlCatalogRoutingAndFinalizeSemantics(t *testing.T) {
	tests := map[string]GraphRouteMode{
		Check:                GraphRouteMergeDeps,
		Fanout:               GraphRouteControlFor,
		RetryEval:            GraphRouteRetryEvalSubject,
		ReviewQuorumFinalize: GraphRouteFallback,
		ReviewQuorumPlan:     GraphRouteFallback,
		ScopeCheck:           GraphRouteControlFor,
		WorkflowFinalize:     GraphRouteFallback,
	}
	for kind, want := range tests {
		if got := GraphRouteModeFor(kind); got != want {
			t.Fatalf("GraphRouteModeFor(%q) = %q, want %q", kind, got, want)
		}
	}
	for _, kind := range ControlDispatcherKinds() {
		got := SkipsOrphanedWorkflowRootClose(kind)
		want := kind == WorkflowFinalize
		if got != want {
			t.Fatalf("SkipsOrphanedWorkflowRootClose(%q) = %t, want %t", kind, got, want)
		}
	}
}
