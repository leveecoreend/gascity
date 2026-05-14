package k8s

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestBuildPodStartGateProtocol(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())

	pod, err := buildPod("test-session", runtime.Config{
		Command:   "claude",
		StartGate: "gc hook --claim --start-gate",
		PreStart:  []string{"setup-worktree"},
		Env: map[string]string{
			"GC_AGENT":        "worker",
			"GC_CITY":         "/workspace",
			"GC_SESSION_NAME": "test-session",
		},
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	cmd := pod.Spec.Containers[0].Args[0]
	for _, want := range []string{
		"GC_START_ENV=/tmp/gc-start-gate/env",
		"internal start-gate-env \"$GC_START_ENV\"",
		"/tmp/gc-start-gate/declined",
		"/tmp/gc-start-gate/failed",
		"touch /tmp/gc-pre-start/failed; sleep infinity",
		"# pre_start[0]",
		"tmux new-session",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("pod command missing %q:\n%s", want, cmd)
		}
	}
}

func TestBuildPodStartGatePersistsGCBeadIDInTmuxEnvironment(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())

	pod, err := buildPod("test-session", runtime.Config{
		Command:   "claude",
		StartGate: "gc hook --claim --start-gate",
		Env: map[string]string{
			"GC_AGENT":        "worker",
			"GC_CITY":         "/workspace",
			"GC_SESSION_NAME": "test-session",
		},
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	cmd := pod.Spec.Containers[0].Args[0]
	if !strings.Contains(cmd, `tmux set-environment -t main GC_BEAD_ID "$GC_BEAD_ID"`) {
		t.Fatalf("pod command does not persist GC_BEAD_ID into tmux metadata:\n%s", cmd)
	}
}

func TestBuildPodStartGateLinuxUserPreservesGCBeadIDWithoutShellInterpolation(t *testing.T) {
	p := newProviderWithOps(newFakeK8sOps())

	pod, err := buildPod("test-session", runtime.Config{
		Command:   "claude",
		StartGate: "gc hook --claim --start-gate",
		Env: map[string]string{
			"GC_AGENT":        "worker",
			"GC_CITY":         "/workspace",
			"GC_SESSION_NAME": "test-session",
			"LINUX_USERNAME":  "worker",
		},
	}, p)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	cmd := pod.Spec.Containers[0].Args[0]
	if !strings.Contains(cmd, "su - worker -c") {
		t.Fatalf("pod command missing login su:\n%s", cmd)
	}
	if strings.Contains(cmd, "su -m worker -c") {
		t.Fatalf("pod command preserves the full root environment:\n%s", cmd)
	}
	if !strings.Contains(cmd, podStartGateTmuxEnvPath) {
		t.Fatalf("pod command missing start_gate env handoff:\n%s", cmd)
	}
	if !strings.Contains(cmd, "chmod 0755 "+podStartGateDir) {
		t.Fatalf("pod command does not make start_gate handoff dir traversable by dynamic user:\n%s", cmd)
	}
	if !strings.Contains(cmd, "chmod 0644 "+podStartGateTmuxEnvPath) {
		t.Fatalf("pod command does not make start_gate env handoff file readable by dynamic user:\n%s", cmd)
	}
	if !strings.Contains(cmd, "cp "+podStartGateEnvPath+".sh "+podStartGateTmuxEnvPath) {
		t.Fatalf("pod command does not pass the rendered start_gate env map to dynamic user:\n%s", cmd)
	}
	if strings.Contains(cmd, `env GC_BEAD_ID=\"${GC_BEAD_ID:-}\"`) {
		t.Fatalf("pod command interpolates GC_BEAD_ID into su shell source:\n%s", cmd)
	}
	if !strings.Contains(cmd, `tmux set-environment -t main GC_BEAD_ID \"$GC_BEAD_ID\"`) {
		t.Fatalf("pod command does not persist GC_BEAD_ID into tmux metadata for dynamic user:\n%s", cmd)
	}
}

func TestBuildPodStartGateExportsGCBeadIDFromEnv(t *testing.T) {
	if err := os.RemoveAll(podStartGateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podStartGateDir) })

	script := buildPodStartGateCommand(`printf 'GC_BEAD_ID=bd-owned\n' > "$GC_START_ENV"`, "")
	script = withFakeStartGateEnvGC(t, script)
	script = strings.ReplaceAll(script, "sleep infinity", ":")
	script += `printf '%s' "$GC_BEAD_ID"`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start_gate command failed: %v; output=%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "bd-owned" {
		t.Fatalf("GC_BEAD_ID = %q, want bd-owned", got)
	}
}

func TestBuildPodStartGateDeclineMarksDeclined(t *testing.T) {
	if err := os.RemoveAll(podStartGateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podStartGateDir) })

	script := buildPodStartGateCommand("exit 1", "")
	script = strings.ReplaceAll(script, "sleep infinity", ":")
	script += `printf 'declined=%s failed=%s' "$(test -e ` + podStartGateDeclinedPath + ` && echo yes || echo no)" "$(test -e ` + podStartGateFailedPath + ` && echo yes || echo no)"`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start_gate command failed: %v; output=%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "declined=yes failed=no" {
		t.Fatalf("result = %q, want declined marker only", got)
	}
}

func TestBuildPodStartGateDeclineIgnoresAmbientGCBeadID(t *testing.T) {
	if err := os.RemoveAll(podStartGateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podStartGateDir) })

	script := "GC_BEAD_ID=bd-ambient\nexport GC_BEAD_ID\n" + buildPodStartGateCommand("exit 1", "")
	script = strings.ReplaceAll(script, "sleep infinity", ":")
	script += `printf 'declined=%s failed=%s bead=%s' "$(test -e ` + podStartGateDeclinedPath + ` && echo yes || echo no)" "$(test -e ` + podStartGateFailedPath + ` && echo yes || echo no)" "$GC_BEAD_ID"`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start_gate command failed: %v; output=%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "declined=yes failed=no bead=bd-ambient" {
		t.Fatalf("result = %q, want declined marker only with ambient bead preserved", got)
	}
}

func TestBuildPodStartGateRejectsInvalidEnvBeforeExport(t *testing.T) {
	if err := os.RemoveAll(podStartGateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podStartGateDir) })

	script := buildPodStartGateCommand(`printf 'GC_BEAD_ID=bd-owned\n' > "$GC_START_ENV"`, "")
	script = "GC_BIN=/bin/false\nexport GC_BIN\n" + strings.ReplaceAll(script, "sleep infinity", ":")
	script += `printf 'bead=%s failed=%s' "${GC_BEAD_ID:-}" "$(test -e ` + podStartGateFailedPath + ` && echo yes || echo no)"`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start_gate command failed: %v; output=%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "bead= failed=yes" {
		t.Fatalf("result = %q, want validation failure without GC_BEAD_ID export", got)
	}
}

func TestBuildPodStartGateDoesNotExposeClaimLog(t *testing.T) {
	if err := os.RemoveAll(podStartGateDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podStartGateDir) })

	script := buildPodStartGateCommand(`printf 'GC_BEAD_ID=gc-123\n' > "$GC_START_ENV"`, "")
	if strings.Contains(script, "claims.jsonl") {
		t.Fatalf("start_gate script should not expose claim log:\n%s", script)
	}
}

func withFakeStartGateEnvGC(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gc")
	content := `#!/bin/sh
if [ "$1" != "internal" ] || [ "$2" != "start-gate-env" ]; then
  exit 127
fi
while IFS= read -r line; do
  [ -n "$line" ] || continue
  case "$line" in
    *=*) key=${line%%=*}; value=${line#*=} ;;
    *) echo "expected KEY=VALUE" >&2; exit 1 ;;
  esac
  case "$key" in
    ""|[0-9]*|*[!A-Za-z0-9_]*) echo "invalid env key" >&2; exit 1 ;;
  esac
  printf "%s='%s'\n" "$key" "$value"
  printf "export %s\n" "$key"
done < "$3"
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return "GC_BIN='" + path + "'\nexport GC_BIN\n" + script
}

func TestBuildPodPreStartFailsAnyCommandFailure(t *testing.T) {
	if err := os.RemoveAll(podPreStartDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(podPreStartDir) })

	script := buildPodPreStartCommands([]string{"exit 7"}, "")
	script = strings.ReplaceAll(script, "sleep infinity", "exit 124")
	script += "printf ok"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("failing pre-start command succeeded; output=%s", out)
	}
	if strings.Contains(string(out), "ok") {
		t.Fatalf("failing pre-start command continued after failure; output=%s", out)
	}
	if _, statErr := os.Stat(podPreStartFailedPath); statErr != nil {
		t.Fatalf("failing pre-start command did not mark failure: %v; output=%s", statErr, out)
	}
}
