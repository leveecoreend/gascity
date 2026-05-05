package dispatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTracefWarnsOnceWhenTracePathCannotBeOpened(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "missing", "workflow-trace.log")
	t.Setenv("GC_WORKFLOW_TRACE", tracePath)

	var stderr bytes.Buffer
	restoreWarnings := useDispatchTraceWarnings(&stderr)
	defer restoreWarnings()

	tracef("first write")
	tracef("second write")

	got := stderr.String()
	if count := strings.Count(got, "opening workflow trace"); count != 1 {
		t.Fatalf("warning count = %d, want 1; stderr=%q", count, got)
	}
	if !strings.Contains(got, tracePath) {
		t.Fatalf("stderr = %q, want missing trace path %q", got, tracePath)
	}
}

func TestTracefPrefersWorkflowTraceOverSlingTrace(t *testing.T) {
	tmp := t.TempDir()
	workflowTrace := filepath.Join(tmp, "workflow-trace.log")
	slingTrace := filepath.Join(tmp, "sling-trace.log")
	t.Setenv("GC_WORKFLOW_TRACE", workflowTrace)
	t.Setenv("GC_SLING_TRACE", slingTrace)

	tracef("prefer workflow trace")

	workflowBytes, err := os.ReadFile(workflowTrace)
	if err != nil {
		t.Fatalf("read workflow trace: %v", err)
	}
	if !strings.Contains(string(workflowBytes), "prefer workflow trace") {
		t.Fatalf("workflow trace = %q, want trace payload", workflowBytes)
	}
	if _, err := os.Stat(slingTrace); !os.IsNotExist(err) {
		t.Fatalf("sling trace should stay unused when GC_WORKFLOW_TRACE is set; stat err=%v", err)
	}
}
