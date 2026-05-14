package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestHookNoWork(t *testing.T) {
	runner := func(string, string) (string, error) { return "", nil }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHook(no work) = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestHookClaimExitCodesDistinguishNoWorkAndFailure(t *testing.T) {
	claimer := &fakeHookClaimExecutor{}

	var noWorkOut, noWorkErr bytes.Buffer
	noWorkRunner := func(string, string) (string, error) { return "", nil }
	noWorkCode := doHookClaim("bd ready", "", noWorkRunner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &noWorkOut, &noWorkErr)
	if noWorkCode != hookClaimExitNoWork {
		t.Fatalf("doHookClaim(no work) = %d, want %d; stderr=%s", noWorkCode, hookClaimExitNoWork, noWorkErr.String())
	}

	var failureOut, failureErr bytes.Buffer
	failureRunner := func(string, string) (string, error) { return "", errors.New("store unavailable") }
	failureCode := doHookClaim("bd ready", "", failureRunner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &failureOut, &failureErr)
	if failureCode != hookClaimExitFailure {
		t.Fatalf("doHookClaim(failure) = %d, want %d", failureCode, hookClaimExitFailure)
	}
	if !strings.Contains(failureErr.String(), "store unavailable") {
		t.Fatalf("stderr = %q, want hard failure", failureErr.String())
	}
}

func TestCmdHookClaimPreservesHardFailureExitCode(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf not-json"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	if err := os.WriteFile(fakeBD, []byte("#!/bin/sh\nprintf '[]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")

	var stdout, stderr bytes.Buffer
	code := run([]string{"hook", "--claim"}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("gc hook --claim exit = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--claim requires JSON work_query output") {
		t.Fatalf("stderr = %q, want hard hook failure", stderr.String())
	}
}

func TestCmdHookClaimMissingSessionContextIsHardFailure(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")

	var stdout, stderr bytes.Buffer
	code := run([]string{"hook", "--claim"}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("gc hook --claim exit = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--claim requires runtime session context") {
		t.Fatalf("stderr = %q, want runtime session context failure", stderr.String())
	}
}

func TestCmdHookInjectClaimConflictIsHardFailure(t *testing.T) {
	clearGCEnv(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"hook", "--inject", "--claim"}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("gc hook --inject --claim exit = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--inject and --claim cannot be used together") {
		t.Fatalf("stderr = %q, want flag conflict", stderr.String())
	}
}

func TestCmdHookStartGateRequiresClaimIsHardFailure(t *testing.T) {
	clearGCEnv(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"hook", "--start-gate"}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("gc hook --start-gate exit = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--start-gate requires --claim") {
		t.Fatalf("stderr = %q, want flag conflict", stderr.String())
	}
}

func TestHookHasWork(t *testing.T) {
	runner := func(string, string) (string, error) { return "hw-1  open  Fix the bug\n", nil }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(has work) = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "hw-1") {
		t.Errorf("stdout = %q, want to contain %q", stdout.String(), "hw-1")
	}
}

func TestHookClaimClaimsUnassignedWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open","metadata":{"gc.routed_to":"worker"}}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1", Metadata: map[string]string{"gc.routed_to": "worker"}},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1", "worker"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-1:session-1" {
		t.Fatalf("claimed = %#v, want bd-1 claimed by session-1", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-1" || items[0].Assignee != "session-1" || items[0].Status != "in_progress" {
		t.Fatalf("items = %#v, want claimed bd-1", items)
	}
}

func TestHookClaimReturnsAssignedInProgressWithoutClaim(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"in_progress","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1", "worker"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no claim for existing in-progress work", claimer.claimed)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-1" {
		t.Fatalf("items = %#v, want existing bd-1", items)
	}
}

func TestHookClaimRevalidatesAssignedInProgressBeforeReturning(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-stale","status":"in_progress","assignee":"session-1"},
			{"id":"bd-new","status":"open"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-stale": {ID: "bd-stale", Status: "in_progress", Assignee: "other-session"},
			"bd-new":   {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-new:session-1" {
		t.Fatalf("claimed = %#v, want stale row skipped and bd-new claimed", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-new" {
		t.Fatalf("items = %#v, want bd-new", items)
	}
}

func TestHookClaimClaimsRevalidatedUnassignedCurrentWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-current","status":"in_progress","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showResults: map[string]hookWorkItem{
			"bd-current": {ID: "bd-current", Status: "open"},
		},
		claimResults: map[string]hookWorkItem{
			"bd-current": {ID: "bd-current", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-current:session-1" {
		t.Fatalf("claimed = %#v, want revalidated bd-current claimed", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-current" {
		t.Fatalf("items = %#v, want bd-current", items)
	}
}

func TestHookClaimClaimsRevalidatedAssignedOpenCurrentWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-current","status":"in_progress","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showResults: map[string]hookWorkItem{
			"bd-current": {ID: "bd-current", Status: "open", Assignee: "session-1"},
		},
		claimResults: map[string]hookWorkItem{
			"bd-current": {ID: "bd-current", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-current:session-1" {
		t.Fatalf("claimed = %#v, want revalidated assigned-open bd-current claimed", got)
	}
}

func TestHookClaimOwnedInProgressRevalidationFailureIsHardFailure(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-current","status":"in_progress","assignee":"session-1"},
			{"id":"bd-new","status":"open"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showErrors: map[string]error{
			"bd-current": errors.New("store unavailable"),
		},
		claimResults: map[string]hookWorkItem{
			"bd-new": {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim() = %d, want hard failure; stderr=%s", code, stderr.String())
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no new claim while current work is unresolved", claimer.claimed)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "revalidating owned in-progress candidates") {
		t.Fatalf("stderr = %q, want revalidation failure", stderr.String())
	}
}

func TestHookClaimStartGateDeclinesOnOwnedInProgressRevalidationFailure(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-current","status":"in_progress","assignee":"session-1"},
			{"id":"bd-new","status":"open"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showErrors: map[string]error{
			"bd-current": errors.New("store unavailable"),
		},
		claimResults: map[string]hookWorkItem{
			"bd-new": {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}
	envPath := filepath.Join(t.TempDir(), "start.env")

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGate:        true,
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != hookClaimExitNoWork {
		t.Fatalf("doHookClaim() = %d, want clean decline; stderr=%s", code, stderr.String())
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no new claim while current work is unresolved", claimer.claimed)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("GC_START_ENV exists after decline: %v", err)
	}
}

func TestHookClaimSessionWorkQueryDoesNotCapAssignedCandidates(t *testing.T) {
	query := prependHookClaimSessionWorkQuery("bd ready --json --limit=20")
	if strings.Contains(query, "--limit=1") {
		t.Fatalf("session work prequery still caps assigned candidates at one:\n%s", query)
	}
	for _, want := range []string{
		`if [ -n "$GC_SESSION_ID" ]; then set -- "$GC_SESSION_ID" "$GC_SESSION_NAME"; else set -- "$GC_SESSION_NAME"; fi`,
		`bd list --status in_progress --assignee="$id" --exclude-type=epic --json`,
		`bd ready --assignee="$id" --exclude-type=epic --json`,
		`done; set --; bd ready --json --limit=20`,
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("session work prequery missing %q:\n%s", want, query)
		}
	}
	if !strings.Contains(query, "bd ready --json --limit=20") {
		t.Fatalf("configured fallback query was not preserved:\n%s", query)
	}
}

func TestHookClaimSessionWorkQueryFallsBackAfterEmptyPrequeryAndClearsArgs(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1 $2 $3" in
  "list --status in_progress")
    printf '[]'
    exit 0
    ;;
  "ready --assignee=gc-session-1")
    printf '[]'
    exit 0
    ;;
  "ready --assignee=worker-session")
    printf '[]'
    exit 0
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	query := prependHookClaimSessionWorkQuery(`test "$#" -eq 0 || { printf 'bad argc:%s' "$#"; exit 7; }; printf '[{"id":"bd-fallback","status":"open"}]'`)
	env := append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GC_SESSION_ID=gc-session-1",
		"GC_SESSION_NAME=worker-session",
	)

	out, err := shellWorkQueryWithEnv(query, "", env)
	if err != nil {
		t.Fatalf("shellWorkQueryWithEnv: %v; out=%q", err, out)
	}
	if strings.TrimSpace(out) != `[{"id":"bd-fallback","status":"open"}]` {
		t.Fatalf("out = %q, want configured fallback JSON", out)
	}
}

func TestHookClaimSessionWorkQueryFailsClosedWhenAssignedPrequeryFails(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1 $2 $3" in
  "list --status in_progress")
    exit 42
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	query := prependHookClaimSessionWorkQuery(`printf '[{"id":"bd-fallback","status":"open"}]'`)
	env := append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GC_SESSION_ID=gc-session-1",
		"GC_SESSION_NAME=worker-session",
	)

	out, err := shellWorkQueryWithEnv(query, "", env)
	if err == nil {
		t.Fatalf("shellWorkQueryWithEnv unexpectedly succeeded with out=%q", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("out = %q, want no fallback output after assigned lookup failure", out)
	}
}

func TestHookClaimSessionWorkQueryFailsClosedWhenAssignedPrequeryIsNonJSON(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1 $2 $3" in
  "list --status in_progress")
    printf 'warning: store warming'
    exit 0
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	query := prependHookClaimSessionWorkQuery(`printf '[{"id":"bd-fallback","status":"open"}]'`)
	env := append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GC_SESSION_ID=gc-session-1",
		"GC_SESSION_NAME=worker-session",
	)

	out, err := shellWorkQueryWithEnv(query, "", env)
	if err == nil {
		t.Fatalf("shellWorkQueryWithEnv unexpectedly succeeded with out=%q", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("out = %q, want no fallback output after uncertain assigned lookup", out)
	}
}

func TestHookClaimFailsWhenOnlyCandidateErrorsRemain(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-current","status":"in_progress","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showErrors: map[string]error{
			"bd-current": errors.New("store unavailable"),
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim() = %d, want hard failure when candidate errors are the only outcome", code)
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no claims after revalidation failure", claimer.claimed)
	}
	if !strings.Contains(stderr.String(), "revalidating owned in-progress bead bd-current") {
		t.Fatalf("stderr = %q, want revalidation error", stderr.String())
	}
}

func TestHookClaimPrefersAssignedInProgressOverUnassignedWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-new","status":"open"},
			{"id":"bd-current","status":"in_progress","assignee":"session-1"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-current": {ID: "bd-current", Status: "in_progress", Assignee: "session-1"},
			"bd-new":     {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1", "worker"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no claim for existing in-progress work", claimer.claimed)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-current" {
		t.Fatalf("items = %#v, want existing bd-current", items)
	}
}

func TestHookClaimPrefersAssignedWorkOverUnassignedWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-new","status":"open"},
			{"id":"bd-assigned","status":"open","assignee":"session-1"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-assigned": {ID: "bd-assigned", Status: "in_progress", Assignee: "session-1"},
			"bd-new":      {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd list", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1", "worker"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-assigned:session-1" {
		t.Fatalf("claimed = %#v, want bd-assigned claimed by session-1", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-assigned" {
		t.Fatalf("items = %#v, want assigned bd-assigned", items)
	}
}

func TestHookClaimSkipsNonClaimableRows(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-closed","status":"closed"},{"id":"bd-open","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-open" || items[0].Assignee != "session-1" {
		t.Fatalf("items = %#v, want bd-open claimed by runtime session", items)
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-open:session-1" {
		t.Fatalf("claimed = %#v, want only bd-open claimed", got)
	}
}

func TestHookClaimSkipsClaimFailureAndClaimsLaterWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[
			{"id":"bd-stale","status":"open"},
			{"id":"bd-new","status":"open"}
		]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimErrors: map[string]error{
			"bd-stale": errors.New("candidate changed"),
		},
		claimResults: map[string]hookWorkItem{
			"bd-new": {ID: "bd-new", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want later viable work claimed; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 2 || got[0] != "bd-stale:session-1" || got[1] != "bd-new:session-1" {
		t.Fatalf("claimed = %#v, want stale attempt then bd-new", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-new" {
		t.Fatalf("items = %#v, want bd-new", items)
	}
}

func TestHookClaimStartGateWritesActiveBeadEnvForAssignedOpenClaim(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-assigned","status":"open","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-assigned": {ID: "bd-assigned", Status: "in_progress", Assignee: "session-1"},
		},
	}
	envPath := filepath.Join(t.TempDir(), "start.env")

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-assigned:session-1" {
		t.Fatalf("claimed = %#v, want bd-assigned claimed by session-1", got)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", envPath, err)
	}
	if string(data) != "GC_BEAD_ID=bd-assigned\n" {
		t.Fatalf("start_gate env = %q, want GC_BEAD_ID handoff", data)
	}
}

func TestHookClaimStartGateHidesClaimInternals(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-assigned","status":"ready","assignee":"session-1"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-assigned": {ID: "bd-assigned", Status: "in_progress", Assignee: "session-1"},
		},
	}
	envPath := filepath.Join(t.TempDir(), "start.env")

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", envPath, err)
	}
	if strings.Contains(string(data), "claimed") || strings.Contains(string(data), "previous") || strings.Contains(string(data), "preserve") {
		t.Fatalf("start_gate env leaked claim internals: %q", data)
	}
}

func TestHookClaimClaimsLegacyPlainIDOutput(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `bd-1 ready task`, nil
	}
	claimer := &fakeHookClaimExecutor{
		showResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "open"},
		},
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim(legacy plain id) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-1:session-1" {
		t.Fatalf("claimed = %#v, want bd-1 claimed by session-1", got)
	}
}

func TestHookClaimLegacyParserIgnoresNonLeadingIDLikeText(t *testing.T) {
	items := parseLegacyHookWorkItems("warning error-1234 while checking bd-1\n")
	if len(items) != 0 {
		t.Fatalf("items = %#v, want no legacy item from non-leading token", items)
	}
}

func TestHookClaimLegacyParserRequiresBeadIDPrefix(t *testing.T) {
	items := parseLegacyHookWorkItems("error-1234 ready task\n")
	if len(items) != 0 {
		t.Fatalf("items = %#v, want no legacy item for unsupported prefix", items)
	}
}

func TestHookClaimStartGateDoesNotWriteEnvForFailedClaim(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open"}]`, nil
	}
	envPath := filepath.Join(t.TempDir(), "start.env")
	claimer := &fakeHookClaimExecutor{
		claimErrors: map[string]error{
			"bd-1": errors.New("store unavailable"),
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim(start_gate failed claim) = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if len(claimer.claimed) != 1 {
		t.Fatalf("claimed = %#v, want one claim", claimer.claimed)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("start_gate env stat err=%v, want no env for failed claim", err)
	}
}

func TestHookClaimStartGateWritesLegacyAlreadyOwnedWork(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `bd-1 ready task`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimAlreadyOwned: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1"},
		},
	}
	envPath := filepath.Join(t.TempDir(), "start.env")

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim(start_gate legacy already-owned) = %d, want 0; stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", envPath, err)
	}
	if string(data) != "GC_BEAD_ID=bd-1\n" {
		t.Fatalf("start_gate env = %q, want GC_BEAD_ID bd-1", data)
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no claim for revalidated already-owned legacy work", claimer.claimed)
	}
}

func TestHookClaimIgnoresBdOwnerFieldWhenAssigneeMissing(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open","owner":"julianknutsen@users.noreply.github.com"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1", Owner: "julianknutsen@users.noreply.github.com"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-1:session-1" {
		t.Fatalf("claimed = %#v, want bd-1 claimed by session-1", got)
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-1" || items[0].Assignee != "session-1" || items[0].Owner != "" {
		t.Fatalf("items = %#v, want canonical assignee without owner", items)
	}
}

func TestHookClaimStartGateDeclinesWithoutWork(t *testing.T) {
	runner := func(string, string) (string, error) { return `[]`, nil }
	envPath := filepath.Join(t.TempDir(), "start.env")

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, &fakeHookClaimExecutor{}, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != hookClaimExitNoWork {
		t.Fatalf("doHookClaim(start_gate no work) = %d, want %d; stderr=%s", code, hookClaimExitNoWork, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty in start_gate mode", stdout.String())
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("start_gate env stat err=%v, want no env when no work", err)
	}
}

func TestHookClaimStartGateRetriesClaimConflicts(t *testing.T) {
	runs := 0
	runner := func(string, string) (string, error) {
		runs++
		if runs == 1 {
			return `[{"id":"bd-1","status":"open"}]`, nil
		}
		return `[{"id":"bd-2","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimConflicts: map[string]bool{"bd-1": true},
		claimResults: map[string]hookWorkItem{
			"bd-2": {ID: "bd-2", Status: "in_progress", Assignee: "session-1"},
		},
	}
	envPath := filepath.Join(t.TempDir(), "start.env")

	oldSleep := hookClaimRetrySleep
	hookClaimRetrySleep = func(time.Duration) {}
	t.Cleanup(func() { hookClaimRetrySleep = oldSleep })

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready --limit=1", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGateEnvPath: envPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim(start_gate claim conflict retry) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if runs != 2 {
		t.Fatalf("work query runs = %d, want 2", runs)
	}
	if got := claimer.claimed; len(got) != 2 || got[0] != "bd-1:session-1" || got[1] != "bd-2:session-1" {
		t.Fatalf("claimed = %#v, want bd-1 conflict then bd-2 success", got)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", envPath, err)
	}
	if string(data) != "GC_BEAD_ID=bd-2\n" {
		t.Fatalf("start_gate env = %q, want GC_BEAD_ID bd-2", data)
	}
}

func TestHookClaimStartGateWithoutEnvPathFailsBeforeQuery(t *testing.T) {
	runner := func(string, string) (string, error) { return `[]`, nil }

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, &fakeHookClaimExecutor{}, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
		StartGate:  true,
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim(start_gate no env path) = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when start_gate env is missing", stdout.String())
	}
	if !strings.Contains(stderr.String(), "GC_START_ENV is required") {
		t.Fatalf("stderr = %q, want missing GC_START_ENV failure", stderr.String())
	}
}

func TestHookClaimStartGateWithoutEnvPathDoesNotClaim(t *testing.T) {
	runs := 0
	runner := func(string, string) (string, error) {
		runs++
		if runs == 1 {
			return `[{"id":"bd-1","status":"open"}]`, nil
		}
		return `[{"id":"bd-2","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimConflicts: map[string]bool{"bd-1": true},
		claimResults: map[string]hookWorkItem{
			"bd-2": {ID: "bd-2", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready --limit=1", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
		StartGate:  true,
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim(start_gate no env path retry) = %d, want %d; stderr=%s", code, hookClaimExitFailure, stderr.String())
	}
	if runs != 0 {
		t.Fatalf("work query runs = %d, want no query before GC_START_ENV validation", runs)
	}
	if got := claimer.claimed; len(got) != 0 {
		t.Fatalf("claimed = %#v, want no claims before GC_START_ENV validation", got)
	}
	if !strings.Contains(stderr.String(), "GC_START_ENV is required") {
		t.Fatalf("stderr = %q, want missing GC_START_ENV failure", stderr.String())
	}
}

func TestHookClaimStartGatePreflightsEnvPathBeforeClaim(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1"},
		},
	}
	var stdout, stderr bytes.Buffer

	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:         "session-1",
		Identities:       []string{"session-1"},
		StartGate:        true,
		StartGateEnvPath: filepath.Join(t.TempDir(), "missing", "env"),
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim() = %d, want failure; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if len(claimer.claimed) != 0 {
		t.Fatalf("claimed = %#v, want no claim before env preflight succeeds", claimer.claimed)
	}
	if !strings.Contains(stderr.String(), "GC_START_ENV") {
		t.Fatalf("stderr = %q, want env path failure", stderr.String())
	}
}

func TestHookClaimDoesNotPreassignContinuationGroupSiblings(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open","metadata":{"gc.routed_to":"worker","gc.continuation_group":"main"}}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimResults: map[string]hookWorkItem{
			"bd-1": {ID: "bd-1", Status: "in_progress", Assignee: "session-1", Metadata: map[string]string{
				"gc.routed_to":          "worker",
				"gc.continuation_group": "main",
			}},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(claimer.claimed) != 1 {
		t.Fatalf("claimed = %#v, want only the selected bead claimed", claimer.claimed)
	}
}

func TestHookClaimStopsAfterAcceptedClaimWithUncertainReadback(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open"},{"id":"bd-2","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimAcceptedErrors: map[string]error{
			"bd-1": fmt.Errorf("reading claimed bead: timeout"),
		},
		claimResults: map[string]hookWorkItem{
			"bd-2": {ID: "bd-2", Status: "in_progress", Assignee: "session-1"},
		},
		showErrors: map[string]error{
			"bd-1": fmt.Errorf("still cannot read bd-1"),
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim() = %d, want failure; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-1:session-1" {
		t.Fatalf("claimed = %#v, want only first uncertain claim attempted", got)
	}
	if !strings.Contains(stderr.String(), "accepted claim") {
		t.Fatalf("stderr = %q, want accepted claim uncertainty", stderr.String())
	}
}

func TestHookClaimStopsAfterAcceptedClaimWithNoResult(t *testing.T) {
	runner := func(string, string) (string, error) {
		return `[{"id":"bd-1","status":"open"},{"id":"bd-2","status":"open"}]`, nil
	}
	claimer := &fakeHookClaimExecutor{
		claimAcceptedNoResults: map[string]bool{
			"bd-1": true,
		},
		claimResults: map[string]hookWorkItem{
			"bd-2": {ID: "bd-2", Status: "in_progress", Assignee: "session-1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready", "", runner, claimer, hookClaimOptions{
		Assignee:   "session-1",
		Identities: []string{"session-1"},
	}, &stdout, &stderr)
	if code != hookClaimExitFailure {
		t.Fatalf("doHookClaim() = %d, want failure; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := claimer.claimed; len(got) != 1 || got[0] != "bd-1:session-1" {
		t.Fatalf("claimed = %#v, want only first uncertain claim attempted", got)
	}
	if !strings.Contains(stderr.String(), "accepted claim") {
		t.Fatalf("stderr = %q, want accepted claim uncertainty", stderr.String())
	}
}

func TestHookCommandError(t *testing.T) {
	runner := func(string, string) (string, error) { return "", fmt.Errorf("command failed") }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHook(error) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "command failed") {
		t.Errorf("stderr = %q, want to contain %q", stderr.String(), "command failed")
	}
}

type fakeHookClaimExecutor struct {
	claimResults           map[string]hookWorkItem
	claimAlreadyOwned      map[string]hookWorkItem
	claimConflicts         map[string]bool
	claimAcceptedErrors    map[string]error
	claimAcceptedNoResults map[string]bool
	claimErrors            map[string]error
	showResults            map[string]hookWorkItem
	showErrors             map[string]error
	onClaim                func(beadID string)
	claimed                []string
}

func (f *fakeHookClaimExecutor) Claim(_ context.Context, _ string, beadID, assignee string) (hookWorkItem, bool, bool, error) {
	if f.onClaim != nil {
		f.onClaim(beadID)
	}
	f.claimed = append(f.claimed, beadID+":"+assignee)
	if err, ok := f.claimAcceptedErrors[beadID]; ok {
		return hookWorkItem{}, true, false, err
	}
	if f.claimAcceptedNoResults[beadID] {
		return hookWorkItem{}, true, false, nil
	}
	if err, ok := f.claimErrors[beadID]; ok {
		return hookWorkItem{}, false, false, err
	}
	if f.claimConflicts[beadID] {
		return hookWorkItem{}, false, false, nil
	}
	if item, ok := f.claimAlreadyOwned[beadID]; ok {
		return item, false, true, nil
	}
	if item, ok := f.claimResults[beadID]; ok {
		return item, true, true, nil
	}
	return hookWorkItem{ID: beadID, Status: "in_progress", Assignee: assignee}, true, true, nil
}

func (f *fakeHookClaimExecutor) Show(_ context.Context, _ string, beadID string) (hookWorkItem, error) {
	if err, ok := f.showErrors[beadID]; ok {
		return hookWorkItem{}, err
	}
	if item, ok := f.showResults[beadID]; ok {
		return item, nil
	}
	if item, ok := f.claimAlreadyOwned[beadID]; ok {
		return item, nil
	}
	if item, ok := f.claimResults[beadID]; ok {
		return item, nil
	}
	return hookWorkItem{ID: beadID}, nil
}

func TestHookInjectNoWork(t *testing.T) {
	runner := func(string, string) (string, error) { return "", nil }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", true, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(inject, no work) = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestHookNoReadyMessagePrintsButExitsOne(t *testing.T) {
	runner := func(string, string) (string, error) {
		return "✨ No ready work found (all issues have blocking dependencies)\n", nil
	}
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHook(no-ready-message) = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "No ready work found") {
		t.Errorf("stdout = %q, want no-ready message", stdout.String())
	}
}

func TestHookInjectSuppressesNoReadyMessage(t *testing.T) {
	runner := func(string, string) (string, error) {
		return "✨ No ready work found (all issues have blocking dependencies)\n", nil
	}
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", true, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(inject, no-ready-message) = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestHookClaimNoReadyMessageIsSilent(t *testing.T) {
	runner := func(string, string) (string, error) {
		return "✨ No ready work found (all issues have blocking dependencies)\n", nil
	}
	var stdout, stderr bytes.Buffer
	code := doHookClaim("bd ready --json", "", runner, &fakeHookClaimExecutor{}, hookClaimOptions{
		Assignee: "worker-session",
	}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHookClaim(no-ready-message) = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestHookInjectIsNonIntrusiveWithWork(t *testing.T) {
	runner := func(string, string) (string, error) { return "hw-1  open  Fix the bug\n", nil }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", true, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(inject, work) = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty non-intrusive inject output", stdout.String())
	}
}

func TestHookInjectDoesNotRunWorkQuery(t *testing.T) {
	called := false
	runner := func(string, string) (string, error) {
		called = true
		return "hw-1  open  Fix the bug\n", nil
	}
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", true, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(inject, work) = %d, want 0", code)
	}
	if called {
		t.Fatal("inject mode ran the work query even though its output is ignored")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty non-intrusive inject output", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestHookCommandCodexInjectDoesNotBlockStop(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"hw-1\",\"title\":\"Fix the bug\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY", cityDir)

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--inject", "--hook-format", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook command failed: %v; stderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty non-blocking Stop hook output", stdout.String())
	}
}

func TestHookCommandInjectSkipsConfiguredWorkQuery(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "work-query-ran")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf ran > %q"
`, marker)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY", cityDir)

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--inject", "--hook-format", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook command failed: %v; stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("inject mode ran configured work_query; marker stat err=%v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty non-blocking Stop hook output", stdout.String())
	}
}

func TestHookCommandHookFormatIsIgnoredForNonInjectOutput(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"hw-1\",\"title\":\"Fix the bug\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY", cityDir)

	run := func(args ...string) (string, string, error) {
		var stdout, stderr bytes.Buffer
		cmd := newHookCmd(&stdout, &stderr)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return stdout.String(), stderr.String(), err
	}

	rawOut, rawErr, err := run("worker")
	if err != nil {
		t.Fatalf("gc hook worker failed: %v; stderr=%s", err, rawErr)
	}
	formattedOut, formattedErr, err := run("worker", "--hook-format", "codex")
	if err != nil {
		t.Fatalf("gc hook worker --hook-format codex failed: %v; stderr=%s", err, formattedErr)
	}
	if formattedOut != rawOut {
		t.Fatalf("hook-format changed non-inject output:\nraw:       %q\nformatted: %q", rawOut, formattedOut)
	}
	if formattedErr != rawErr {
		t.Fatalf("hook-format changed non-inject stderr:\nraw:       %q\nformatted: %q", rawErr, formattedErr)
	}
	if strings.Contains(formattedOut, "system-reminder") {
		t.Fatalf("non-inject hook output was provider-formatted: %q", formattedOut)
	}
}

func TestCmdHookClaimRequiresRuntimeSessionContext(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"bd-1\",\"status\":\"open\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY", cityDir)

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("gc hook worker --claim succeeded without runtime session context")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--claim requires runtime session context") {
		t.Fatalf("stderr = %q, want runtime session context error", stderr.String())
	}
}

func TestCmdHookClaimIgnoresAgentAliasAsOwnershipIdentity(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"bd-current\",\"status\":\"in_progress\",\"assignee\":\"worker\"},{\"id\":\"bd-new\",\"status\":\"open\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := fmt.Sprintf(`#!/bin/sh
	printf '%%s\n' "$*" >> %q
	case "$1 $2 $3" in
	  "list --status in_progress")
	    printf '[]'
	    exit 0
	    ;;
	  "ready --assignee=session-1 --exclude-type=epic")
	    printf '[]'
	    exit 0
	    ;;
	  "update --json bd-new")
	    exit 0
	    ;;
  "show --json bd-new")
    printf '[{"id":"bd-new","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
  "show --json bd-current")
    printf '[{"id":"bd-current","status":"in_progress","assignee":"worker"}]'
    exit 0
    ;;
esac
exit 1
`, logPath)
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_ALIAS", "worker")
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-new" || items[0].Assignee != "session-1" {
		t.Fatalf("items = %#v, want bd-new claimed by runtime session", items)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	if !strings.Contains(string(logData), "update --json bd-new --claim") {
		t.Fatalf("bd log = %q, want bd-new claim", string(logData))
	}
	if strings.Contains(string(logData), "show --json bd-current") {
		t.Fatalf("bd log = %q, should not adopt alias-assigned bd-current", string(logData))
	}
}

func TestCmdHookClaimPrefersSessionIDAsAssignee(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"bd-new\",\"status\":\"open\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := fmt.Sprintf(`#!/bin/sh
	printf 'actor=%%s args=%%s\n' "${BEADS_ACTOR:-}" "$*" >> %q
	case "$1 $2 $3" in
	  "list --status in_progress")
	    printf '[]'
	    exit 0
	    ;;
	  "ready --assignee=gc-session-1 --exclude-type=epic")
	    printf '[]'
	    exit 0
	    ;;
	  "ready --assignee=worker-session --exclude-type=epic")
	    printf '[]'
	    exit 0
	    ;;
	  "update --json bd-new")
	    [ "${BEADS_ACTOR:-}" = "gc-session-1" ] || exit 3
	    exit 0
    ;;
  "show --json bd-new")
    printf '[{"id":"bd-new","status":"in_progress","assignee":"gc-session-1"}]'
    exit 0
    ;;
esac
exit 1
`, logPath)
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "worker-session")
	t.Setenv("GC_SESSION_ID", "gc-session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-new" || items[0].Assignee != "gc-session-1" {
		t.Fatalf("items = %#v, want bd-new claimed by canonical session ID", items)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	if !strings.Contains(string(logData), "actor=gc-session-1") {
		t.Fatalf("bd log = %q, want BEADS_ACTOR=gc-session-1", string(logData))
	}
}

func TestCmdHookClaimWithSessionIDAdoptsLegacySessionNameOwnedWork(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	marker := filepath.Join(t.TempDir(), "configured-query-ran")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = %q
`, "printf ran > "+marker+"; printf '[{\"id\":\"bd-new\",\"status\":\"open\"}]'")
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := fmt.Sprintf(`#!/bin/sh
printf 'actor=%%s args=%%s\n' "${BEADS_ACTOR:-}" "$*" >> %q
case "$1 $2 $3 $4" in
  "list --status in_progress --assignee=gc-session-1")
    printf '[]'
    exit 0
    ;;
  "ready --assignee=gc-session-1 --exclude-type=epic")
    printf 'No ready work found'
    exit 0
    ;;
  "list --status in_progress --assignee=worker-session")
    printf '[{"id":"bd-name","status":"in_progress","assignee":"worker-session"}]'
    exit 0
    ;;
esac
case "$1 $2 $3" in
  "show --json bd-name")
    printf '[{"id":"bd-name","status":"in_progress","assignee":"worker-session"}]'
    exit 0
    ;;
  "update --json bd-new")
    [ "${BEADS_ACTOR:-}" = "gc-session-1" ] || exit 3
    exit 0
    ;;
  "show --json bd-new")
    printf '[{"id":"bd-new","status":"in_progress","assignee":"gc-session-1"}]'
    exit 0
    ;;
esac
printf '[]'
`, logPath)
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "worker-session")
	t.Setenv("GC_SESSION_ID", "gc-session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-name" || items[0].Assignee != "worker-session" {
		t.Fatalf("items = %#v, want legacy session-name owned bd-name", items)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured work_query marker stat err=%v, want not created", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	if !strings.Contains(string(logData), "assignee=worker-session") {
		t.Fatalf("bd log = %q, want legacy session-name query after canonical ID has no work", string(logData))
	}
	if strings.Contains(string(logData), "args=update --json bd-new --claim") {
		t.Fatalf("bd log = %q, must not claim new work before legacy owned work", string(logData))
	}
}

func TestHookCommandErrorPrintsPartialOutput(t *testing.T) {
	runner := func(string, string) (string, error) {
		return "[]\n", fmt.Errorf("timed out after 15s with partial stdout")
	}
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", false, runner, &stdout, &stderr)
	if code != 1 {
		t.Errorf("doHook(error with output) = %d, want 1", code)
	}
	if got := stdout.String(); got != "[]" {
		t.Errorf("stdout = %q, want partial JSON output", got)
	}
	if !strings.Contains(stderr.String(), "partial stdout") {
		t.Errorf("stderr = %q, want timeout diagnostic", stderr.String())
	}
}

func TestShellWorkQueryWithEnvTimeoutReportsPartialOutput(t *testing.T) {
	oldTimeout := hookWorkQueryTimeout
	hookWorkQueryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hookWorkQueryTimeout = oldTimeout })

	out, err := shellWorkQueryWithEnv("printf '[]\\n'; sleep 1", "", nil)
	if err == nil {
		t.Fatal("shellWorkQueryWithEnv() error = nil, want timeout")
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("stdout = %q, want partial JSON output", out)
	}
	if !strings.Contains(err.Error(), "partial stdout") {
		t.Fatalf("error = %v, want partial stdout diagnostic", err)
	}
}

func TestCmdHookClaimStartGateWritesEnvFromCLI(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = "printf '[{\"id\":\"bd-1\",\"status\":\"open\"}]'"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 0
    ;;
  show)
    printf '[{"id":"bd-1","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")
	envPath := filepath.Join(t.TempDir(), "start.env")
	t.Setenv("GC_START_ENV", envPath)

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim", "--start-gate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim --start-gate failed: %v; stderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty in start_gate mode", stdout.String())
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", envPath, err)
	}
	if string(data) != "GC_BEAD_ID=bd-1\n" {
		t.Fatalf("start_gate env = %q, want claimed bead env", data)
	}
}

func TestShellHookClaimExecutorPreservesPreparedRuntimeEnv(t *testing.T) {
	fakeBin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "bd-env.log")
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
{
  printf 'cmd=%s\n' "$*"
  printf 'BEADS_DIR=%s\n' "${BEADS_DIR:-}"
  printf 'BEADS_DOLT_SERVER_HOST=%s\n' "${BEADS_DOLT_SERVER_HOST:-}"
  printf 'BEADS_DOLT_SERVER_PORT=%s\n' "${BEADS_DOLT_SERVER_PORT:-}"
  printf 'BEADS_DOLT_PASSWORD=%s\n' "${BEADS_DOLT_PASSWORD:-}"
  printf 'GC_CITY_PATH=%s\n' "${GC_CITY_PATH:-}"
  printf 'BEADS_ACTOR=%s\n' "${BEADS_ACTOR:-}"
} >> "$GC_CAPTURE"
case "$1" in
  update)
    exit 0
    ;;
  show)
    printf '[{"id":"bd-1","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)

	claimer := shellHookClaimExecutor{env: []string{
		"PATH=" + pathValue,
		"GC_CAPTURE=" + capturePath,
		"BEADS_DIR=/managed/beads",
		"BEADS_DOLT_SERVER_HOST=managed-db.example.com",
		"BEADS_DOLT_SERVER_PORT=3307",
		"BEADS_DOLT_PASSWORD=secret-value",
		"GC_CITY_PATH=/city/root",
	}}
	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if claimed.ID != "bd-1" || claimed.Assignee != "session-1" {
		t.Fatalf("claimed = %#v, want bd-1 assigned to session-1", claimed)
	}
	if !claimedNow {
		t.Fatal("Claim() claimedNow = false, want true")
	}

	logData, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", capturePath, err)
	}
	logText := string(logData)
	for _, want := range []string{
		"BEADS_DIR=/managed/beads",
		"BEADS_DOLT_SERVER_HOST=managed-db.example.com",
		"BEADS_DOLT_SERVER_PORT=3307",
		"BEADS_DOLT_PASSWORD=secret-value",
		"GC_CITY_PATH=/city/root",
		"BEADS_ACTOR=session-1",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("bd env log missing %q:\n%s", want, logText)
		}
	}
}

func TestShellHookClaimExecutorDoesNotAdoptAssignedOpenAfterClaimFailure(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 1
    ;;
  show)
    printf '[{"id":"bd-1","status":"open","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue}}

	claimed, _, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if ok {
		t.Fatalf("Claim() ok = true with claimed %#v, want false for assigned open bead", claimed)
	}
}

func TestShellHookClaimExecutorReportsAlreadyOwnedClaimAsNotNew(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 1
    ;;
  show)
    printf '[{"id":"bd-1","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue}}

	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true for already-owned in-progress bead")
	}
	if claimedNow {
		t.Fatal("Claim() claimedNow = true, want false for already-owned in-progress bead")
	}
	if claimed.ID != "bd-1" || claimed.Assignee != "session-1" || claimed.Status != "in_progress" {
		t.Fatalf("claimed = %#v, want existing bd-1", claimed)
	}
}

func TestShellHookClaimExecutorReportsAcceptedClaimReadFailureAsNew(t *testing.T) {
	fakeBin := t.TempDir()
	countPath := filepath.Join(t.TempDir(), "show-count")
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 0
    ;;
  show)
    count=0
    if [ -f "$GC_SHOW_COUNT" ]; then
      count=$(cat "$GC_SHOW_COUNT")
    fi
    count=$((count + 1))
    printf '%s' "$count" > "$GC_SHOW_COUNT"
    if [ "$count" = "1" ]; then
      exit 2
    fi
    printf '[{"id":"bd-1","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue, "GC_SHOW_COUNT=" + countPath}}

	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true after accepted claim read failure")
	}
	if !claimedNow {
		t.Fatal("Claim() claimedNow = false, want true because bd accepted the claim update")
	}
	if claimed.ID != "bd-1" || claimed.Assignee != "session-1" || claimed.Status != "in_progress" {
		t.Fatalf("claimed = %#v, want claimed bd-1", claimed)
	}
}

func TestShellHookClaimExecutorAcceptsClaimBeforeStatusConverges(t *testing.T) {
	fakeBin := t.TempDir()
	countPath := filepath.Join(t.TempDir(), "show-count")
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 0
    ;;
  show)
    count=0
    if [ -f "$GC_SHOW_COUNT" ]; then
      count=$(cat "$GC_SHOW_COUNT")
    fi
    count=$((count + 1))
    printf '%s' "$count" > "$GC_SHOW_COUNT"
    if [ "$count" = "1" ]; then
      exit 2
    fi
    printf '[{"id":"bd-1","status":"ready","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue, "GC_SHOW_COUNT=" + countPath}}

	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true after accepted claim read failure")
	}
	if !claimedNow {
		t.Fatal("Claim() claimedNow = false, want true because bd accepted the claim update")
	}
	if claimed.ID != "bd-1" || claimed.Assignee != "session-1" || claimed.Status != "in_progress" {
		t.Fatalf("claimed = %#v, want normalized in-progress bd-1", claimed)
	}
}

func TestShellHookClaimExecutorNormalizesSuccessfulStaleClaimRead(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 0
    ;;
  show)
    printf '[{"id":"bd-1","status":"ready","assignee":"session-1"}]'
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue}}

	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true after accepted claim")
	}
	if !claimedNow {
		t.Fatal("Claim() claimedNow = false, want true because bd accepted the claim update")
	}
	if claimed.ID != "bd-1" || claimed.Assignee != "session-1" || claimed.Status != "in_progress" {
		t.Fatalf("claimed = %#v, want normalized in-progress bd-1", claimed)
	}
}

func TestShellHookClaimExecutorAcceptedClaimNotFoundIsHardFailure(t *testing.T) {
	fakeBin := t.TempDir()
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1" in
  update)
    exit 0
    ;;
  show)
    echo "issue not found" >&2
    exit 1
    ;;
esac
exit 1
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathValue)
	claimer := shellHookClaimExecutor{env: []string{"PATH=" + pathValue}}

	claimed, claimedNow, ok, err := claimer.Claim(context.Background(), t.TempDir(), "bd-1", "session-1")
	if err == nil {
		t.Fatalf("Claim() error = nil with claimed=%#v claimedNow=%v ok=%v, want hard failure", claimed, claimedNow, ok)
	}
	if ok {
		t.Fatalf("Claim() ok = true with error %v", err)
	}
	if errors.Is(err, beads.ErrNotFound) {
		t.Fatalf("Claim() error wraps ErrNotFound and would be treated as stale candidate: %v", err)
	}
	if !strings.Contains(err.Error(), "accepted claim") {
		t.Fatalf("Claim() error = %v, want accepted-claim context", err)
	}
}

func TestCmdHookClaimChecksSessionWorkBeforeConfiguredQuery(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "custom-query-ran")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = %q
`, "printf custom > "+marker+"; printf '[]'")
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1 $2 $3" in
  "show --json bd-current")
    printf '[{"id":"bd-current","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
case "$1 $2 $3 $4" in
  "list --status in_progress --assignee=session-1")
    printf '[{"id":"bd-current","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-current" {
		t.Fatalf("items = %#v, want bd-current from session lookup", items)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("custom work_query marker stat err=%v, want not exist", err)
	}
}

func TestCmdHookClaimFallsBackToConfiguredQueryWhenSessionLookupHasNoWork(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "custom-query-ran")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[agent]]
name = "worker"
work_query = %q
`, "printf custom > "+marker+"; printf '[{\"id\":\"bd-custom\",\"status\":\"open\"}]'")
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := `#!/bin/sh
case "$1 $2 $3" in
  "show --json bd-custom")
    printf '[{"id":"bd-custom","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
  "update --json bd-custom")
    exit 0
    ;;
esac
case "$1 $2 $3 $4" in
  "list --status in_progress --assignee=session-1")
    printf '[]'
    exit 0
    ;;
  "ready --assignee=session-1 --exclude-type=epic --json")
    printf '✨ No ready work found (all issues have blocking dependencies)'
    exit 0
    ;;
esac
printf '[]'
`
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-custom" {
		t.Fatalf("items = %#v, want bd-custom from configured query", items)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("custom work_query marker stat err=%v, want created", err)
	}
}

func TestCmdHookClaimDefaultQueryDoesNotUseAliasAssignee(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
for arg in "$@"; do
  if [ "$arg" = "--assignee=worker" ]; then
    printf '[{"id":"bd-alias","status":"open","assignee":"worker"}]'
    exit 0
  fi
done
case "$1 $2 $3" in
  "update --json bd-routed")
    exit 0
    ;;
  "show --json bd-routed")
    printf '[{"id":"bd-routed","status":"in_progress","assignee":"session-1"}]'
    exit 0
    ;;
esac
case "$1 $2" in
  "ready --metadata-field")
    printf '[{"id":"bd-routed","status":"open"}]'
    exit 0
    ;;
esac
printf '[]'
`, logPath)
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_ALIAS", "worker")
	t.Setenv("GC_AGENT", "worker")
	t.Setenv("GC_SESSION_NAME", "session-1")

	var stdout, stderr bytes.Buffer
	cmd := newHookCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"worker", "--claim"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc hook worker --claim failed: %v; stderr=%s", err, stderr.String())
	}
	var items []hookWorkItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("claim output is not JSON array: %v; out=%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].ID != "bd-routed" || items[0].Assignee != "session-1" {
		t.Fatalf("items = %#v, want routed work claimed by runtime session", items)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	if strings.Contains(string(logData), "--assignee=worker") {
		t.Fatalf("bd log = %q, default claim query should not scan GC_ALIAS assignee", string(logData))
	}
}

func TestCmdHookSessionTemplateContextDoesNotScanSessionsForName(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	cityDir := t.TempDir()
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBD := filepath.Join(fakeBin, "bd")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nprintf '[]'\n", logPath)
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_TEMPLATE", "worker")
	t.Setenv("GC_ALIAS", "worker-1")
	t.Setenv("GC_SESSION_ID", "mc-session")
	t.Setenv("GC_SESSION_NAME", "runtime-session")

	var stdout, stderr bytes.Buffer
	code := cmdHookWithFormat(nil, false, "", &stdout, &stderr)
	if code != 1 {
		t.Fatalf("cmdHookWithFormat() = %d, want 1 for empty work; stderr=%s", code, stderr.String())
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	logText := string(logData)
	if strings.Contains(logText, "--label=gc:session") {
		t.Fatalf("gc hook scanned all session beads before running work_query:\n%s", logText)
	}
	if !strings.Contains(logText, "--assignee=runtime-session") {
		t.Fatalf("gc hook did not pass runtime session name into work_query; bd log:\n%s", logText)
	}
}

func TestHookInjectAlwaysExitsZero(t *testing.T) {
	// Even on command failure, inject mode exits 0.
	runner := func(string, string) (string, error) { return "", fmt.Errorf("command failed") }
	var stdout, stderr bytes.Buffer
	code := doHook("bd ready", "", true, runner, &stdout, &stderr)
	if code != 0 {
		t.Errorf("doHook(inject, error) = %d, want 0", code)
	}
}

func TestHookPassesWorkQuery(t *testing.T) {
	// Verify the runner receives the correct work query string.
	var receivedCmd, receivedDir string
	runner := func(cmd, dir string) (string, error) {
		receivedCmd = cmd
		receivedDir = dir
		return "item-1\n", nil
	}
	var stdout, stderr bytes.Buffer
	doHook("bd ready --assignee=mayor", "/tmp/work", false, runner, &stdout, &stderr)
	if receivedCmd != "bd ready --assignee=mayor" {
		t.Errorf("runner command = %q, want %q", receivedCmd, "bd ready --assignee=mayor")
	}
	if receivedDir != "/tmp/work" {
		t.Errorf("runner dir = %q, want %q", receivedDir, "/tmp/work")
	}
}

func TestShellWorkQueryTimesOutPromptly(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	oldTimeout := hookWorkQueryTimeout
	hookWorkQueryTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		hookWorkQueryTimeout = oldTimeout
	})

	start := time.Now()
	_, err := shellWorkQueryWithEnv("sleep 5", t.TempDir(), nil)
	if err == nil {
		t.Fatal("shellWorkQueryWithEnv(sleep) err = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout diagnostic", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("shellWorkQueryWithEnv timeout elapsed %s, want under 1s", elapsed)
	}
}

func TestWorkQueryHasReadyWorkEmptyJSONArray(t *testing.T) {
	if workQueryHasReadyWork("[]") {
		t.Fatal("workQueryHasReadyWork([]) = true, want false")
	}
}

func TestWorkQueryHasReadyWorkNonEmptyJSONArray(t *testing.T) {
	if !workQueryHasReadyWork(`[{"id":"abc"}]`) {
		t.Fatal("workQueryHasReadyWork(non-empty array) = false, want true")
	}
}

func TestCmdHookUsesAgentCityAndRigRoot(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	rigDir := filepath.Join(cityDir, "myrig-repo")
	workDir := filepath.Join(cityDir, ".gc", "worktrees", "myrig", "polecat-1")
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "myrig"
path = %q

[[agent]]
name = "polecat"
dir = "myrig"

[agent.pool]
min = 0
max = 5
`, rigDir)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'pwd=%s\nstore_root=%s\nstore_scope=%s\nprefix=%s\nrig=%s\nrig_root=%s\nargs=%s\n' \"$PWD\" \"${GC_STORE_ROOT:-}\" \"${GC_STORE_SCOPE:-}\" \"${GC_BEADS_PREFIX:-}\" \"${GC_RIG:-}\" \"${GC_RIG_ROOT:-}\" \"$*\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "myrig/polecat")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cmdHook(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "pwd="+rigDir) {
		t.Fatalf("stdout = %q, want command to run from rig root %q", out, rigDir)
	}
	if !strings.Contains(out, "store_root="+rigDir) {
		t.Fatalf("stdout = %q, want GC_STORE_ROOT=%q", out, rigDir)
	}
	if !strings.Contains(out, "store_scope=rig") {
		t.Fatalf("stdout = %q, want GC_STORE_SCOPE=rig", out)
	}
	if !strings.Contains(out, "prefix=my") {
		t.Fatalf("stdout = %q, want GC_BEADS_PREFIX=my", out)
	}
	if !strings.Contains(out, "rig=myrig") {
		t.Fatalf("stdout = %q, want GC_RIG=myrig", out)
	}
	if !strings.Contains(out, "rig_root="+rigDir) {
		t.Fatalf("stdout = %q, want GC_RIG_ROOT=%q", out, rigDir)
	}
	// Tiered query: first tier checks in_progress assigned to session name.
	if !strings.Contains(out, "args=list --status in_progress --assignee=host-session --exclude-type=epic --json --limit=1") {
		t.Fatalf("stdout = %q, want pool work_query args", out)
	}
}

// TestCmdHookOverridesInheritedCityBeadsDir is a regression test for #514:
// when the gc hook process inherits a city-scoped BEADS_DIR from its parent,
// the work query subprocess must still run against the rig-scoped bead store
// for rig-backed agents. Without the fix, the subprocess reads the city
// store and returns [] for rig-routed work.
func TestCmdHookOverridesInheritedCityBeadsDir(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	rigDir := filepath.Join(cityDir, "myrig-repo")
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "myrig"
path = %q

[[agent]]
name = "worker"
dir = "myrig"
`, rigDir)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'beads_dir=%s\\nrig_root=%s\\nrig=%s\\n' \"$BEADS_DIR\" \"$GC_RIG_ROOT\" \"$GC_RIG\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_DIR", rigDir)
	// Pollute parent env with a city-scoped BEADS_DIR. Without the fix,
	// this value leaks into the fake-bd subprocess and the hook reads the
	// city store instead of the rig store.
	cityBeads := filepath.Join(cityDir, ".beads")
	t.Setenv("BEADS_DIR", cityBeads)

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	wantBeads := filepath.Join(rigDir, ".beads")
	if !strings.Contains(out, "beads_dir="+wantBeads) {
		t.Fatalf("stdout = %q, want BEADS_DIR=%s (rig store), not inherited city value", out, wantBeads)
	}
	if strings.Contains(out, "beads_dir="+cityBeads) {
		t.Fatalf("stdout = %q, inherited city BEADS_DIR leaked into subprocess", out)
	}
	if !strings.Contains(out, "rig_root="+rigDir) {
		t.Fatalf("stdout = %q, want GC_RIG_ROOT=%s", out, rigDir)
	}
	if !strings.Contains(out, "rig=myrig") {
		t.Fatalf("stdout = %q, want GC_RIG=myrig", out)
	}
}

// TestCmdHookResolvesRelativeRigPath guards the relative-rig-path handling:
// when `[[rigs]].path` is relative (e.g. "myrig-repo"), cmdHook must
// normalize it to an absolute path before building the rig env, or
// BEADS_DIR/GC_RIG_ROOT land as relative garbage and bdRuntimeEnvForRig's
// rig-matching loop misses the rig entirely (skipping GC_RIG and any
// per-rig Dolt overrides).
func TestCmdHookResolvesRelativeRigPath(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	rigAbs := filepath.Join(cityDir, "myrig-repo")
	if err := os.MkdirAll(rigAbs, 0o755); err != nil {
		t.Fatal(err)
	}
	// Relative rig path — the fix normalizes this to cityDir/myrig-repo.
	cityToml := `[workspace]
name = "test-city"

[[rigs]]
name = "myrig"
path = "myrig-repo"

[[agent]]
name = "worker"
dir = "myrig"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'beads_dir=%s\\nrig_root=%s\\nrig=%s\\n' \"$BEADS_DIR\" \"$GC_RIG_ROOT\" \"$GC_RIG\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_DIR", rigAbs)

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	wantBeads := filepath.Join(rigAbs, ".beads")
	if !strings.Contains(out, "beads_dir="+wantBeads) {
		t.Fatalf("stdout = %q, want absolute BEADS_DIR=%s (relative rig path should be resolved)", out, wantBeads)
	}
	if !strings.Contains(out, "rig_root="+rigAbs) {
		t.Fatalf("stdout = %q, want absolute GC_RIG_ROOT=%s", out, rigAbs)
	}
	// GC_RIG is only set when bdRuntimeEnvForRig's loop finds a matching
	// rig config. With unresolved relative paths, samePath() fails and
	// GC_RIG stays empty — this assertion catches that regression.
	if !strings.Contains(out, "rig=myrig") {
		t.Fatalf("stdout = %q, want GC_RIG=myrig (rig-matching loop must find the rig)", out)
	}
}

func TestCmdHookExpandsTemplateCommandsWithCityFallback(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := filepath.Join(t.TempDir(), "demo-city")
	rigDir := filepath.Join(cityDir, "frontend")
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[[rigs]]
name = "frontend"
path = %q

[[agent]]
name = "worker"
dir = "frontend"
work_query = "bd {{.CityName}} {{.Rig}} {{.AgentBase}}"
`, rigDir)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'args=%s\\n' \"$*\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_DIR", rigDir)

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "args=demo-city frontend worker") {
		t.Fatalf("stdout = %q, want expanded city/rig/agent-base template", stdout.String())
	}
}

// TestCmdHookNonRigDirAgentUsesCityStore guards the rig-detection heuristic
// in hookQueryEnv: agents whose `dir` is a plain path (not a configured
// rig) must fall back to the city-scoped bead store, not mistakenly be
// treated as rig-backed and pointed at `<dir>/.beads`.
func TestCmdHookNonRigDirAgentUsesCityStore(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cityDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No [[rigs]] section — "workdir" is a plain agent dir, not a rig.
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
dir = "workdir"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'beads_dir=%s\\nrig_root=%s\\n' \"$BEADS_DIR\" \"$GC_RIG_ROOT\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	wantBeads := filepath.Join(cityDir, ".beads")
	if !strings.Contains(out, "beads_dir="+wantBeads) {
		t.Fatalf("stdout = %q, want BEADS_DIR=%s (city store), non-rig agent must not be pointed at <dir>/.beads", out, wantBeads)
	}
	// Non-rig agents must not receive GC_RIG_ROOT. doHook strips trailing
	// whitespace, so the empty value lands at the very end of the output.
	if !strings.HasSuffix(out, "rig_root=") {
		t.Fatalf("stdout = %q, want empty GC_RIG_ROOT for non-rig agent", out)
	}
}

func TestCmdHookPoolInstanceUsesTemplatePoolLabel(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	rigDir := filepath.Join(cityDir, "myrig-repo")
	workDir := filepath.Join(cityDir, ".gc", "worktrees", "myrig", "polecat-1")
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "myrig"
path = %q

[[agent]]
name = "polecat"
dir = "myrig"

[agent.pool]
min = 0
max = 5
`, rigDir)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'pwd=%s\\nargs=%s\\n' \"$PWD\" \"$*\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_AGENT", "myrig/polecat-1")
	t.Setenv("GC_SESSION_NAME", "myrig--polecat-1")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cmdHook(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "pwd="+rigDir) {
		t.Fatalf("stdout = %q, want command to run from rig root %q", out, rigDir)
	}
	// Tiered query: first tier checks in_progress assigned to session name.
	if !strings.Contains(out, "args=list --status in_progress --assignee=host-session --exclude-type=epic --json --limit=1") {
		t.Fatalf("stdout = %q, want pool template work_query args", out)
	}
}

func TestWorkQueryEnvForDirOverridesInheritedPWD(t *testing.T) {
	env := []string{
		"PATH=/tmp/bin",
		"PWD=/tmp/stale",
		"GC_CITY=/tmp/city",
	}

	got := workQueryEnvForDir(env, "/tmp/rig")

	if strings.Contains(strings.Join(got, "\n"), "PWD=/tmp/stale") {
		t.Fatalf("workQueryEnvForDir preserved stale PWD: %v", got)
	}
	if !strings.Contains(strings.Join(got, "\n"), "PWD=/tmp/rig") {
		t.Fatalf("workQueryEnvForDir missing updated PWD: %v", got)
	}
	if !strings.Contains(strings.Join(got, "\n"), "PATH=/tmp/bin") {
		t.Fatalf("workQueryEnvForDir dropped unrelated env: %v", got)
	}
}

func TestCmdHookExportsResolvedIdentityForFixedAgentQuery(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := `[workspace]
name = "test-city"

[[agent]]
name = "worker"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'agent=%s\\nsession=%s\\nargs=%s\\n' \"$GC_AGENT\" \"$GC_SESSION_NAME\" \"$*\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "agent=worker") {
		t.Fatalf("stdout = %q, want GC_AGENT=worker", out)
	}
	if !strings.Contains(out, "session=host-session") {
		t.Fatalf("stdout = %q, want GC_SESSION_NAME=host-session", out)
	}
	// Tiered query: first tier checks in_progress assigned to session name.
	if !strings.Contains(out, `args=list --status in_progress --assignee=host-session --exclude-type=epic --json --limit=1`) {
		t.Fatalf("stdout = %q, want metadata-routed work query", out)
	}
}

func TestCmdHookExportsResolvedIdentityFromRigContext(t *testing.T) {
	clearGCEnv(t)
	disableManagedDoltRecoveryForTest(t)
	t.Setenv("GC_TMUX_SESSION", "host-session")
	cityDir := t.TempDir()
	rigDir := filepath.Join(cityDir, "myrig-repo")
	fakeBin := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cityToml := fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "myrig"
path = %q

[[agent]]
name = "worker"
dir = "myrig"
`, rigDir)
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(cityToml), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBD := filepath.Join(fakeBin, "bd")
	script := "#!/bin/sh\nprintf 'agent=%s\\nsession=%s\\nargs=%s\\n' \"$GC_AGENT\" \"$GC_SESSION_NAME\" \"$*\"\n"
	if err := os.WriteFile(fakeBD, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+origPath)
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_DIR", rigDir)

	wantAgent := "myrig/worker"
	wantSession := cliSessionName(cityDir, "test-city", wantAgent, "")

	var stdout, stderr bytes.Buffer
	code := cmdHook([]string{"worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "agent="+wantAgent) {
		t.Fatalf("stdout = %q, want GC_AGENT=%s", out, wantAgent)
	}
	if !strings.Contains(out, "session="+wantSession) {
		t.Fatalf("stdout = %q, want GC_SESSION_NAME=%s", out, wantSession)
	}
	// Tiered query: first tier checks in_progress assigned to session name.
	if !strings.Contains(out, `args=list --status in_progress --assignee=host-session --exclude-type=epic --json --limit=1`) {
		t.Fatalf("stdout = %q, want metadata-routed work query", out)
	}
}

func TestDoHookNormalizesSingleObjectOutputToArray(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := func(_, _ string) (string, error) {
		return `{"id":"bd-1","title":"Work"}`, nil
	}

	code := doHook("bd ready", ".", false, runner, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHook() = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != `[{"id":"bd-1","title":"Work"}]` {
		t.Fatalf("stdout = %q, want normalized JSON array", got)
	}
}
