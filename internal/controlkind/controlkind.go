// Package controlkind centralizes metadata about gc.kind values used by
// graph.v2 compilation, routing, and control dispatch.
package controlkind

import (
	"sort"
	"strings"
)

const (
	Check                = "check"
	Cleanup              = "cleanup"
	Fanout               = "fanout"
	Ralph                = "ralph"
	Retry                = "retry"
	RetryEval            = "retry-eval"
	RetryRun             = "retry-run"
	ReviewQuorumFinalize = "review-quorum-finalize"
	ReviewQuorumPlan     = "review-quorum-plan"
	Run                  = "run"
	Scope                = "scope"
	ScopeCheck           = "scope-check"
	Spec                 = "spec"
	Workflow             = "workflow"
	WorkflowFinalize     = "workflow-finalize"
)

type GraphRouteMode string

const (
	GraphRouteDefault          GraphRouteMode = ""
	GraphRouteControlFor       GraphRouteMode = "control_for"
	GraphRouteFallback         GraphRouteMode = "fallback"
	GraphRouteRetryEvalSubject GraphRouteMode = "retry_eval_subject"
	GraphRouteMergeDeps        GraphRouteMode = "merge_deps"
)

// RuntimeRequirements describes caller-supplied dependencies a control handler
// needs beyond the bead store itself.
type RuntimeRequirements struct {
	NeedsCityConfig                 bool
	NeedsFormulaSearchPaths         bool
	NeedsPrepareFragment            bool
	NeedsSessionRecycle             bool
	NeedsSourceWorkflowCoordination bool
}

type KindSpec struct {
	Kind                          string
	flags                         flags
	Runtime                       RuntimeRequirements
	GraphRouteMode                GraphRouteMode
	SkipOrphanedWorkflowRootClose bool
}

type flags uint16

const (
	controlDispatcher flags = 1 << iota
	workflowTopology
	requiresGraphContract
	detachedGraphStep
	scopeCheckExempt
	ralphOutputExempt
	dynamicScopeControl
	latestAttemptCandidateExempt
)

const (
	baseControl     = controlDispatcher | requiresGraphContract | dynamicScopeControl | latestAttemptCandidateExempt
	scopedControl   = baseControl | scopeCheckExempt | ralphOutputExempt
	detachedControl = baseControl | detachedGraphStep
)

var (
	fragmentRuntime = RuntimeRequirements{
		NeedsCityConfig:         true,
		NeedsFormulaSearchPaths: true,
		NeedsPrepareFragment:    true,
	}
	retryRuntime = RuntimeRequirements{
		NeedsCityConfig:         true,
		NeedsFormulaSearchPaths: true,
		NeedsSessionRecycle:     true,
	}
	retryEvalRuntime = RuntimeRequirements{
		NeedsCityConfig:     true,
		NeedsSessionRecycle: true,
	}
	workflowFinalizeRuntime = RuntimeRequirements{
		NeedsCityConfig:                 true,
		NeedsSourceWorkflowCoordination: true,
	}
)

// specs is the built-in control catalog for gc.kind metadata. Handler
// functions stay in their owning packages, but graph/runtime semantics live
// here so formulas, routing, dispatch, and lint-style checks share one source.
var specs = map[string]KindSpec{
	Check:                {flags: scopedControl | detachedGraphStep, Runtime: fragmentRuntime, GraphRouteMode: GraphRouteMergeDeps},
	Cleanup:              {flags: requiresGraphContract},
	Fanout:               {flags: scopedControl, Runtime: fragmentRuntime, GraphRouteMode: GraphRouteControlFor},
	Ralph:                {flags: detachedControl | ralphOutputExempt, Runtime: retryRuntime},
	Retry:                {flags: detachedControl, Runtime: retryRuntime},
	RetryEval:            {flags: detachedControl, Runtime: retryEvalRuntime, GraphRouteMode: GraphRouteRetryEvalSubject},
	RetryRun:             {flags: requiresGraphContract | detachedGraphStep},
	ReviewQuorumFinalize: {flags: scopedControl | detachedGraphStep, GraphRouteMode: GraphRouteFallback},
	ReviewQuorumPlan:     {flags: scopedControl | detachedGraphStep, GraphRouteMode: GraphRouteFallback},
	Run:                  {flags: requiresGraphContract | detachedGraphStep},
	Scope:                {flags: workflowTopology | requiresGraphContract | scopeCheckExempt | ralphOutputExempt},
	ScopeCheck:           {flags: scopedControl, GraphRouteMode: GraphRouteControlFor},
	Spec:                 {flags: workflowTopology | scopeCheckExempt | ralphOutputExempt},
	Workflow:             {flags: workflowTopology | latestAttemptCandidateExempt},
	WorkflowFinalize: {
		flags:                         scopedControl,
		Runtime:                       workflowFinalizeRuntime,
		GraphRouteMode:                GraphRouteFallback,
		SkipOrphanedWorkflowRootClose: true,
	},
}

func Lookup(kind string) (KindSpec, bool) {
	key := strings.TrimSpace(kind)
	spec, ok := specs[key]
	spec.Kind = key
	return spec, ok
}

func has(kind string, flag flags) bool {
	spec, ok := Lookup(kind)
	return ok && spec.flags&flag != 0
}

func kindsWith(flag flags) []string {
	kinds := make([]string, 0, len(specs))
	for kind, spec := range specs {
		if spec.flags&flag != 0 {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func IsControlDispatcher(kind string) bool {
	return has(kind, controlDispatcher)
}

func ControlDispatcherKinds() []string {
	return kindsWith(controlDispatcher)
}

func RuntimeRequirementsFor(kind string) (RuntimeRequirements, bool) {
	spec, ok := Lookup(kind)
	if !ok || spec.flags&controlDispatcher == 0 {
		return RuntimeRequirements{}, false
	}
	return spec.Runtime, true
}

func GraphRouteModeFor(kind string) GraphRouteMode {
	spec, ok := Lookup(kind)
	if !ok {
		return GraphRouteDefault
	}
	return spec.GraphRouteMode
}

func SkipsOrphanedWorkflowRootClose(kind string) bool {
	spec, ok := Lookup(kind)
	return ok && spec.SkipOrphanedWorkflowRootClose
}

func IsWorkflowTopology(kind string) bool {
	return has(kind, workflowTopology)
}

func RequiresGraphContract(kind string) bool {
	return has(kind, requiresGraphContract)
}

func IsDetachedGraphStep(kind string) bool {
	return has(kind, detachedGraphStep)
}

func IsScopeCheckExempt(kind string) bool {
	return has(kind, scopeCheckExempt)
}

func IsRalphOutputExempt(kind string) bool {
	return has(kind, ralphOutputExempt)
}

func IsDynamicScopeControl(kind string) bool {
	return has(kind, dynamicScopeControl)
}

func IsLatestAttemptCandidateExempt(kind string) bool {
	return has(kind, latestAttemptCandidateExempt)
}
