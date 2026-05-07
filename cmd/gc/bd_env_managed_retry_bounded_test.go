package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

// TestManagedRetry_DoesNotExceedSingleCallDeadline verifies that when
// bounded retry is enabled, a transport-retryable error returned AFTER
// the per-call subprocess timeout has elapsed does NOT trigger a retry.
// The architecture parent (ga-f4m2) flagged this as the source of the
// 4× compounding observed in the spawn-hang RCA — without this gate,
// two calls × two retries = up to 8× the per-call budget held inside
// reopenClosedConfiguredNamedSessionBead.
//
// Architecture: ga-f4m2.1.
func TestManagedRetry_DoesNotExceedSingleCallDeadline(t *testing.T) {
	t.Setenv("GC_BEADS", "bd")

	origRunnerFactory := beadsNewExecCommandRunner
	origRecover := recoverManagedBDCommand
	t.Cleanup(func() {
		beadsNewExecCommandRunner = origRunnerFactory
		recoverManagedBDCommand = origRecover
	})

	timeout := 80 * time.Millisecond
	attempts := 0
	recoverCalls := 0

	beadsNewExecCommandRunner = func(opts ...beads.RunnerOpt) beads.CommandRunner {
		return func(_ string, _ string, _ ...string) ([]byte, error) {
			attempts++
			// Simulate first attempt exhausting its subprocess budget
			// before returning a transport error.
			time.Sleep(timeout + 25*time.Millisecond)
			return nil, errors.New("server unreachable at 127.0.0.1:3307")
		}
	}
	recoverManagedBDCommand = func(_ string) error {
		recoverCalls++
		return nil
	}

	runner := bdCommandRunnerWithManagedRetryOpts(
		t.TempDir(),
		func(_ string) map[string]string {
			return map[string]string{"GC_DOLT_PORT": "3307"}
		},
		timeout,
		true,
	)

	start := time.Now()
	_, err := runner(t.TempDir(), "bd", "update", "x", "--status=closed")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runner unexpectedly succeeded")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (bounded retry must skip when budget exceeded)", attempts)
	}
	if recoverCalls != 0 {
		t.Fatalf("recoverCalls = %d, want 0 (bounded retry must not invoke recovery when skipping)", recoverCalls)
	}
	if elapsed >= 2*timeout {
		t.Fatalf("elapsed = %s, want < 2× timeout (%s) to prove no retry happened", elapsed, 2*timeout)
	}
}

// TestManagedRetry_RetriesWithinBudget verifies that when bounded retry
// is enabled but the first attempt completed WITHIN the per-call
// timeout, a transport-retryable error still triggers the standard
// recovery + retry path. This locks in that bounded retry only
// suppresses retries that would blow past the budget, not all retries.
//
// Architecture: ga-f4m2.1.
func TestManagedRetry_RetriesWithinBudget(t *testing.T) {
	t.Setenv("GC_BEADS", "bd")

	origRunnerFactory := beadsNewExecCommandRunner
	origRecover := recoverManagedBDCommand
	t.Cleanup(func() {
		beadsNewExecCommandRunner = origRunnerFactory
		recoverManagedBDCommand = origRecover
	})

	timeout := 200 * time.Millisecond
	attempts := 0
	recoverCalls := 0

	beadsNewExecCommandRunner = func(opts ...beads.RunnerOpt) beads.CommandRunner {
		return func(_ string, _ string, _ ...string) ([]byte, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("server unreachable at 127.0.0.1:3307")
			}
			return []byte("ok"), nil
		}
	}
	recoverManagedBDCommand = func(_ string) error {
		recoverCalls++
		return nil
	}

	runner := bdCommandRunnerWithManagedRetryOpts(
		t.TempDir(),
		func(_ string) map[string]string {
			return map[string]string{"GC_DOLT_PORT": "3307"}
		},
		timeout,
		true,
	)

	out, err := runner(t.TempDir(), "bd", "list", "--json")
	if err != nil {
		t.Fatalf("runner error = %v, want nil", err)
	}
	if string(out) != "ok" {
		t.Fatalf("runner output = %q, want %q", out, "ok")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if recoverCalls != 1 {
		t.Fatalf("recoverCalls = %d, want 1", recoverCalls)
	}
}

// TestManagedRetry_UnboundedRetriesEvenAfterBudget verifies that when
// bounded retry is OFF (the default for one release per the
// architecture rollout plan), a transport-retryable error retries
// regardless of elapsed wall-clock — preserving today's behavior so
// the flag flip is the only behavior switch.
//
// Architecture: ga-f4m2.1.
func TestManagedRetry_UnboundedRetriesEvenAfterBudget(t *testing.T) {
	t.Setenv("GC_BEADS", "bd")

	origRunnerFactory := beadsNewExecCommandRunner
	origRecover := recoverManagedBDCommand
	t.Cleanup(func() {
		beadsNewExecCommandRunner = origRunnerFactory
		recoverManagedBDCommand = origRecover
	})

	timeout := 50 * time.Millisecond
	attempts := 0
	recoverCalls := 0

	beadsNewExecCommandRunner = func(opts ...beads.RunnerOpt) beads.CommandRunner {
		return func(_ string, _ string, _ ...string) ([]byte, error) {
			attempts++
			if attempts == 1 {
				time.Sleep(timeout + 20*time.Millisecond)
				return nil, errors.New("server unreachable at 127.0.0.1:3307")
			}
			return []byte("ok"), nil
		}
	}
	recoverManagedBDCommand = func(_ string) error {
		recoverCalls++
		return nil
	}

	runner := bdCommandRunnerWithManagedRetryOpts(
		t.TempDir(),
		func(_ string) map[string]string {
			return map[string]string{"GC_DOLT_PORT": "3307"}
		},
		timeout,
		false, // bounded OFF — retry even past budget
	)

	out, err := runner(t.TempDir(), "bd", "list", "--json")
	if err != nil {
		t.Fatalf("runner error = %v, want nil", err)
	}
	if string(out) != "ok" {
		t.Fatalf("runner output = %q, want %q", out, "ok")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if recoverCalls != 1 {
		t.Fatalf("recoverCalls = %d, want 1", recoverCalls)
	}
}
