package formula

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRetriesBasic(t *testing.T) {
	steps := []*Step{
		{
			ID:          "review",
			Title:       "Review change",
			Description: "Run review work.",
			Type:        "task",
			Needs:       []string{"setup"},
			Assignee:    "polecat",
			Labels:      []string{"pool:polecat"},
			Metadata: map[string]string{
				"gc.scope_ref":  "body",
				"gc.scope_role": "member",
				"gc.on_fail":    "abort_scope",
				"custom":        "value",
			},
			Retry: &RetrySpec{
				MaxAttempts: 3,
				OnExhausted: "soft_fail",
			},
		},
	}

	got, err := ApplyRetries(steps)
	if err != nil {
		t.Fatalf("ApplyRetries failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	logical := got[0]
	run := got[1]
	eval := got[2]

	if logical.ID != "review" {
		t.Fatalf("logical.ID = %q, want review", logical.ID)
	}
	if run.ID != "review.run.1" {
		t.Fatalf("run.ID = %q, want review.run.1", run.ID)
	}
	if eval.ID != "review.eval.1" {
		t.Fatalf("eval.ID = %q, want review.eval.1", eval.ID)
	}

	if logical.Metadata["gc.kind"] != "retry" {
		t.Fatalf("logical gc.kind = %q, want retry", logical.Metadata["gc.kind"])
	}
	if logical.Metadata["gc.scope_ref"] != "body" {
		t.Fatalf("logical gc.scope_ref = %q, want body", logical.Metadata["gc.scope_ref"])
	}
	if logical.Metadata["gc.on_fail"] != "abort_scope" {
		t.Fatalf("logical gc.on_fail = %q, want abort_scope", logical.Metadata["gc.on_fail"])
	}
	if len(logical.Needs) != 2 || logical.Needs[0] != "setup" || logical.Needs[1] != "review.eval.1" {
		t.Fatalf("logical.Needs = %v, want [setup review.eval.1]", logical.Needs)
	}

	if run.Metadata["gc.kind"] != "retry-run" {
		t.Fatalf("run gc.kind = %q, want retry-run", run.Metadata["gc.kind"])
	}
	if run.Metadata["gc.attempt"] != "1" {
		t.Fatalf("run gc.attempt = %q, want 1", run.Metadata["gc.attempt"])
	}
	if run.Metadata["gc.on_exhausted"] != "soft_fail" {
		t.Fatalf("run gc.on_exhausted = %q, want soft_fail", run.Metadata["gc.on_exhausted"])
	}
	if run.Metadata["gc.scope_ref"] != "" || run.Metadata["gc.scope_role"] != "" || run.Metadata["gc.on_fail"] != "" {
		t.Fatalf("run scope metadata leaked: %#v", run.Metadata)
	}
	if run.Metadata["custom"] != "value" {
		t.Fatalf("run custom metadata = %q, want value", run.Metadata["custom"])
	}

	if eval.Metadata["gc.kind"] != "retry-eval" {
		t.Fatalf("eval gc.kind = %q, want retry-eval", eval.Metadata["gc.kind"])
	}
	if len(eval.Needs) != 1 || eval.Needs[0] != "review.run.1" {
		t.Fatalf("eval.Needs = %v, want [review.run.1]", eval.Needs)
	}
}

func TestCompileRetryManagedStepBlocksWorkflowOnLogicalBead(t *testing.T) {
	dir := t.TempDir()
	formulaContent := `
formula = "retry-demo"
version = 2

[[steps]]
id = "review"
title = "Review"
assignee = "polecat"
type = "task"

[steps.retry]
max_attempts = 3
on_exhausted = "soft_fail"
`
	if err := os.WriteFile(filepath.Join(dir, "retry-demo.formula.toml"), []byte(formulaContent), 0o644); err != nil {
		t.Fatal(err)
	}

	recipe, err := Compile(context.Background(), "retry-demo", []string{dir}, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var rootID, finalizerID string
	for _, step := range recipe.Steps {
		if step.IsRoot {
			rootID = step.ID
		}
		if step.Metadata["gc.kind"] == "workflow-finalize" {
			finalizerID = step.ID
		}
	}
	if rootID == "" {
		t.Fatal("missing workflow root")
	}
	if finalizerID == "" {
		t.Fatal("missing workflow finalizer")
	}

	var sawLogical, sawRun, sawEval bool
	for _, dep := range recipe.Deps {
		if dep.StepID == rootID && dep.Type == "blocks" && dep.DependsOnID != finalizerID {
			t.Fatalf("workflow root should only block on finalizer, saw %s", dep.DependsOnID)
		}
		if dep.StepID != finalizerID || dep.Type != "blocks" {
			continue
		}
		switch dep.DependsOnID {
		case "retry-demo.review":
			sawLogical = true
		case "retry-demo.review.run.1":
			sawRun = true
		case "retry-demo.review.eval.1":
			sawEval = true
		}
	}
	if !sawLogical {
		t.Fatal("workflow finalizer should block on logical retry bead")
	}
	if sawRun || sawEval {
		t.Fatalf("workflow finalizer should not block directly on retry attempt beads; sawRun=%v sawEval=%v", sawRun, sawEval)
	}
}
