package k8s

import (
	"encoding/base64"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/gastownhall/gascity/internal/runtime"
)

func TestBuildPod_NodeSelector(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	p.nodeSelector = map[string]string{"workload": "gc-agents"}
	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if pod.Spec.NodeSelector["workload"] != "gc-agents" {
		t.Errorf("NodeSelector[workload] = %q, want \"gc-agents\"", pod.Spec.NodeSelector["workload"])
	}
}

func TestBuildPod_Tolerations(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	p.tolerations = []corev1.Toleration{{
		Key: "gc-agents", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
	}}
	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if len(pod.Spec.Tolerations) != 1 {
		t.Fatalf("len(Tolerations) = %d, want 1", len(pod.Spec.Tolerations))
	}
	if pod.Spec.Tolerations[0].Key != "gc-agents" {
		t.Errorf("Toleration.Key = %q, want \"gc-agents\"", pod.Spec.Tolerations[0].Key)
	}
}

func TestBuildPod_Affinity(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	p.affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key: "node-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"gpu"},
					}},
				}},
			},
		},
	}
	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if pod.Spec.Affinity == nil {
		t.Fatal("Affinity is nil")
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		t.Fatal("NodeAffinity is nil")
	}
	expressions := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if expressions[0].Values[0] != "gpu" {
		t.Fatalf("affinity value = %q, want gpu", expressions[0].Values[0])
	}
}

func TestBuildPod_PriorityClassName(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	p.priorityClassName = "gc-agent-high"
	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if pod.Spec.PriorityClassName != "gc-agent-high" {
		t.Errorf("PriorityClassName = %q, want \"gc-agent-high\"", pod.Spec.PriorityClassName)
	}
}

func TestBuildPod_NoSchedulingFields_NoBehaviorChange(t *testing.T) {
	// Zero-value scheduling fields must not alter default pod behavior.
	p := newProviderWithOps(newFakeK8sOps())
	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if pod.Spec.NodeSelector != nil {
		t.Errorf("NodeSelector should be nil when not set")
	}
	if len(pod.Spec.Tolerations) != 0 {
		t.Errorf("Tolerations should be empty when not set")
	}
	if pod.Spec.Affinity != nil {
		t.Errorf("Affinity should be nil when not set")
	}
	if pod.Spec.PriorityClassName != "" {
		t.Errorf("PriorityClassName should be empty when not set")
	}
}

func TestBuildPod_IncludesPromptSuffixInTmuxCommand(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	pod, err := buildPod("test-session", runtime.Config{
		Command:      "agent-cli",
		PromptSuffix: "'Run the startup prompt.'",
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	got := decodedTmuxCommand(t, pod)
	want := "agent-cli 'Run the startup prompt.'"
	if got != want {
		t.Fatalf("tmux command = %q, want %q", got, want)
	}
}

func TestBuildPod_IncludesPromptFlagInTmuxCommand(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	pod, err := buildPod("test-session", runtime.Config{
		Command:      "agent-cli",
		PromptFlag:   "--prompt",
		PromptSuffix: "'Run the startup prompt.'",
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	got := decodedTmuxCommand(t, pod)
	want := "agent-cli --prompt 'Run the startup prompt.'"
	if got != want {
		t.Fatalf("tmux command = %q, want %q", got, want)
	}
}

func TestBuildPod_WritesDecodedCommandToScriptForDynamicUser(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())
	command := `bash -lc 'exec "$GC_CITY_PATH/scripts/gr7n-router-cli" "$@"' gr7n-router-cli`
	pod, err := buildPod("test-session", runtime.Config{
		Command: command,
		WorkDir: "/city/.gc/agents/k8s-canary",
		Env: map[string]string{
			"GC_CITY":        "/city",
			"LINUX_USERNAME": "bryce",
		},
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	got := decodedTmuxCommand(t, pod)
	if got != command {
		t.Fatalf("tmux command = %q, want %q", got, command)
	}
	args := pod.Spec.Containers[0].Args[0]
	if !strings.Contains(args, "/tmp/gc-agent-command.sh") {
		t.Fatalf("pod args do not write/run decoded command script:\n%s", args)
	}
	if strings.Contains(args, `tmux new-session -d -s main "$CMD"`) {
		t.Fatalf("pod args still inline decoded command through nested su shell:\n%s", args)
	}
	if !strings.Contains(args, `tmux new-session -d -s main /bin/bash /tmp/gc-agent-command.sh`) {
		t.Fatalf("pod args do not run decoded command script via tmux:\n%s", args)
	}
}

func TestRemapControllerCommandToPodRemapsSiblingRigRoot(t *testing.T) {
	cfgEnv := map[string]string{
		"GC_CITY":     "/home/bryce/projects/gr7n-city",
		"GC_RIG":      "grustle-monorepo",
		"GC_RIG_ROOT": "/home/bryce/projects/grustle-monorepo",
	}
	cmd := "/home/bryce/projects/gr7n-city/scripts/project-worker-worktree-setup.sh /home/bryce/projects/grustle-monorepo /home/bryce/projects/gr7n-city/.gc/worktrees/grustle-monorepo/web-workers/web-worker-1 web-worker-1 --sync"

	got := remapControllerCommandToPod(cmd, cfgEnv)
	want := "/workspace/scripts/project-worker-worktree-setup.sh /workspace/grustle-monorepo /workspace/.gc/worktrees/grustle-monorepo/web-workers/web-worker-1 web-worker-1 --sync"
	if got != want {
		t.Fatalf("remapped command = %q, want %q", got, want)
	}
}

func decodedTmuxCommand(t *testing.T, pod *corev1.Pod) string {
	t.Helper()
	if len(pod.Spec.Containers) == 0 || len(pod.Spec.Containers[0].Args) == 0 {
		t.Fatal("pod has no agent args")
	}
	args := pod.Spec.Containers[0].Args[0]
	const prefix = "CMD=$(echo '"
	start := strings.Index(args, prefix)
	if start == -1 {
		t.Fatalf("pod args missing encoded command prefix: %s", args)
	}
	start += len(prefix)
	end := strings.Index(args[start:], "' | base64 -d)")
	if end == -1 {
		t.Fatalf("pod args missing encoded command suffix: %s", args)
	}
	decoded, err := base64.StdEncoding.DecodeString(args[start : start+end])
	if err != nil {
		t.Fatalf("decode command: %v", err)
	}
	return string(decoded)
}

func TestBuildPod_ClonesSchedulingFields(t *testing.T) {
	seconds := int64(30)
	p := newProviderWithOps(newFakeK8sOps())
	p.nodeSelector = map[string]string{"workload": "gc-agents"}
	p.tolerations = []corev1.Toleration{{
		Key:               "gc-agents",
		Operator:          corev1.TolerationOpExists,
		Effect:            corev1.TaintEffectNoSchedule,
		TolerationSeconds: &seconds,
	}}
	p.affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key: "node-type", Operator: corev1.NodeSelectorOpIn, Values: []string{"gpu"},
					}},
				}},
			},
		},
	}

	pod, err := buildPod("test-session", runtime.Config{Command: "/bin/bash"}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	pod.Spec.NodeSelector["workload"] = "changed"
	pod.Spec.Tolerations[0].Key = "changed"
	pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values[0] = "changed"

	if p.nodeSelector["workload"] != "gc-agents" {
		t.Fatalf("provider nodeSelector mutated to %q", p.nodeSelector["workload"])
	}
	if p.tolerations[0].Key != "gc-agents" {
		t.Fatalf("provider toleration key mutated to %q", p.tolerations[0].Key)
	}
	values := p.affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values
	if values[0] != "gpu" {
		t.Fatalf("provider affinity value mutated to %q", values[0])
	}
}
