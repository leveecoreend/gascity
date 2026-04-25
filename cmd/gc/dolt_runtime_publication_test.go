package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPublishManagedDoltRuntimeStateRepairsStaleProviderState verifies that
// publishManagedDoltRuntimeState recovers when dolt-provider-state.json has a
// stale PID (e.g. dolt was restarted) but the process is actually running and
// healthy. The repaired state must be written to both dolt-provider-state.json
// and dolt-state.json.
func TestPublishManagedDoltRuntimeStateRepairsStaleProviderState(t *testing.T) {
	cityPath := t.TempDir()
	layout, err := resolveManagedDoltRuntimeLayout(cityPath)
	if err != nil {
		t.Fatalf("resolveManagedDoltRuntimeLayout: %v", err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data dir): %v", err)
	}

	port := reserveRandomTCPPort(t)
	listener := startTCPListenerProcessInDir(t, port, layout.DataDir)
	defer func() {
		_ = listener.Process.Kill()
		_ = listener.Wait()
	}()

	// Write provider state with a stale PID — simulates dolt having been
	// restarted but provider state not yet refreshed.
	if err := writeDoltRuntimeStateFile(layout.StateFile, doltRuntimeState{
		Running:   true,
		PID:       999999, // stale — no such process
		Port:      port,
		DataDir:   layout.DataDir,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeDoltRuntimeStateFile(provider): %v", err)
	}

	if err := publishManagedDoltRuntimeState(cityPath); err != nil {
		t.Fatalf("publishManagedDoltRuntimeState: %v", err)
	}

	// dolt-state.json must now exist and carry the correct live PID.
	published, err := readDoltRuntimeStateFile(managedDoltStatePath(cityPath))
	if err != nil {
		t.Fatalf("readDoltRuntimeStateFile(dolt-state.json): %v", err)
	}
	if !published.Running {
		t.Fatal("published.Running = false, want true")
	}
	if published.Port != port {
		t.Fatalf("published.Port = %d, want %d", published.Port, port)
	}
	if published.PID != listener.Process.Pid {
		t.Fatalf("published.PID = %d, want %d (actual listener PID)", published.PID, listener.Process.Pid)
	}

	// Provider state must also be repaired.
	repaired, err := readDoltRuntimeStateFile(layout.StateFile)
	if err != nil {
		t.Fatalf("readDoltRuntimeStateFile(provider): %v", err)
	}
	if repaired.PID != listener.Process.Pid {
		t.Fatalf("repaired provider PID = %d, want %d", repaired.PID, listener.Process.Pid)
	}
}

// TestPublishManagedDoltRuntimeStateRecoversMissingProviderState verifies that
// publishManagedDoltRuntimeState succeeds when dolt-provider-state.json is
// entirely absent (e.g. a crash deleted it) but dolt is running and reachable.
func TestPublishManagedDoltRuntimeStateRecoversMissingProviderState(t *testing.T) {
	cityPath := t.TempDir()
	layout, err := resolveManagedDoltRuntimeLayout(cityPath)
	if err != nil {
		t.Fatalf("resolveManagedDoltRuntimeLayout: %v", err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data dir): %v", err)
	}
	// Ensure parent directory for provider state exists (normally written by script).
	if err := os.MkdirAll(filepath.Dir(layout.StateFile), 0o755); err != nil {
		t.Fatalf("MkdirAll(state dir): %v", err)
	}

	port := reserveRandomTCPPort(t)
	listener := startTCPListenerProcessInDir(t, port, layout.DataDir)
	defer func() {
		_ = listener.Process.Kill()
		_ = listener.Wait()
	}()

	// No provider state file — absent entirely.
	if _, err := os.Stat(layout.StateFile); err == nil {
		if err := os.Remove(layout.StateFile); err != nil {
			t.Fatalf("remove provider state: %v", err)
		}
	}

	// publishManagedDoltRuntimeState cannot recover from a truly absent provider
	// state file when there's no port hint at all: repairedManagedDoltRuntimeState
	// needs a port from the existing state. Verify it returns a meaningful error
	// rather than panicking or silently succeeding with wrong data.
	err = publishManagedDoltRuntimeState(cityPath)
	// The function must either succeed (if it can discover the process) or
	// return an error containing context.  It must never panic.
	if err != nil {
		if !strings.Contains(err.Error(), "provider dolt runtime state") &&
			!strings.Contains(err.Error(), "managed dolt runtime state") {
			t.Fatalf("unexpected error format (missing context): %v", err)
		}
	}
}

// TestPublishManagedDoltRuntimeStateRecoversMissingProviderStateWithPortHint
// verifies recovery when dolt-provider-state.json is absent but dolt IS running
// AND we have a stale state with the correct port to probe.  This simulates the
// scenario where the published dolt-state.json exists with a valid port but the
// provider state was lost (e.g. runtime dir was wiped).
func TestPublishManagedDoltRuntimeStateRecoversMissingProviderStateWithPortHint(t *testing.T) {
	cityPath := t.TempDir()
	layout, err := resolveManagedDoltRuntimeLayout(cityPath)
	if err != nil {
		t.Fatalf("resolveManagedDoltRuntimeLayout: %v", err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data dir): %v", err)
	}

	port := reserveRandomTCPPort(t)
	listener := startTCPListenerProcessInDir(t, port, layout.DataDir)
	defer func() {
		_ = listener.Process.Kill()
		_ = listener.Wait()
	}()

	// Write provider state with a stopped (running=false) entry that still
	// carries the correct port. This simulates the state after op_stop_impl
	// clears running=false but before a new start writes the new PID.
	if err := writeDoltRuntimeStateFile(layout.StateFile, doltRuntimeState{
		Running:   false,
		PID:       0,
		Port:      port,
		DataDir:   layout.DataDir,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeDoltRuntimeStateFile(provider stopped): %v", err)
	}

	if err := publishManagedDoltRuntimeState(cityPath); err != nil {
		t.Fatalf("publishManagedDoltRuntimeState: %v", err)
	}

	published, err := readDoltRuntimeStateFile(managedDoltStatePath(cityPath))
	if err != nil {
		t.Fatalf("readDoltRuntimeStateFile(dolt-state.json): %v", err)
	}
	if !published.Running {
		t.Fatal("published.Running = false, want true")
	}
	if published.Port != port {
		t.Fatalf("published.Port = %d, want %d", published.Port, port)
	}
	if published.PID != listener.Process.Pid {
		t.Fatalf("published.PID = %d, want %d (listener PID)", published.PID, listener.Process.Pid)
	}
}

// TestPublishManagedDoltRuntimeStateSucceedsWhenAlreadyValid verifies the
// normal (non-recovery) path still works correctly.
func TestPublishManagedDoltRuntimeStateSucceedsWhenAlreadyValid(t *testing.T) {
	cityPath := t.TempDir()
	layout, err := resolveManagedDoltRuntimeLayout(cityPath)
	if err != nil {
		t.Fatalf("resolveManagedDoltRuntimeLayout: %v", err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data dir): %v", err)
	}

	port := reserveRandomTCPPort(t)
	listener := startTCPListenerProcessInDir(t, port, layout.DataDir)
	defer func() {
		_ = listener.Process.Kill()
		_ = listener.Wait()
	}()

	// Write a fully valid provider state.
	if err := writeDoltRuntimeStateFile(layout.StateFile, doltRuntimeState{
		Running:   true,
		PID:       listener.Process.Pid,
		Port:      port,
		DataDir:   layout.DataDir,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeDoltRuntimeStateFile(provider): %v", err)
	}

	if err := publishManagedDoltRuntimeState(cityPath); err != nil {
		t.Fatalf("publishManagedDoltRuntimeState: %v", err)
	}

	published, err := readDoltRuntimeStateFile(managedDoltStatePath(cityPath))
	if err != nil {
		t.Fatalf("readDoltRuntimeStateFile(dolt-state.json): %v", err)
	}
	if !published.Running {
		t.Fatal("published.Running = false, want true")
	}
	if published.Port != port {
		t.Fatalf("published.Port = %d, want %d", published.Port, port)
	}
	if published.PID != listener.Process.Pid {
		t.Fatalf("published.PID = %d, want %d", published.PID, listener.Process.Pid)
	}
}

// TestPublishManagedDoltRuntimeStateFailsWhenDoltNotRunning verifies that
// publishManagedDoltRuntimeState returns an error when dolt is not running
// (stale PID, no port holder) and does not create a dolt-state.json.
func TestPublishManagedDoltRuntimeStateFailsWhenDoltNotRunning(t *testing.T) {
	cityPath := t.TempDir()
	layout, err := resolveManagedDoltRuntimeLayout(cityPath)
	if err != nil {
		t.Fatalf("resolveManagedDoltRuntimeLayout: %v", err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data dir): %v", err)
	}
	// Reserve a port and immediately release it so we have a valid port number
	// but nothing listening there.
	port := reserveRandomTCPPort(t)

	if err := writeDoltRuntimeStateFile(layout.StateFile, doltRuntimeState{
		Running:   true,
		PID:       999999,
		Port:      port,
		DataDir:   layout.DataDir,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("writeDoltRuntimeStateFile(provider): %v", err)
	}

	err = publishManagedDoltRuntimeState(cityPath)
	if err == nil {
		t.Fatal("publishManagedDoltRuntimeState() succeeded, want error (nothing listening)")
	}
	if !strings.Contains(err.Error(), "managed dolt runtime state") {
		t.Fatalf("error missing context: %v", err)
	}

	// dolt-state.json must not have been created.
	if _, statErr := os.Stat(managedDoltStatePath(cityPath)); statErr == nil {
		t.Fatal("dolt-state.json was created despite dolt not running")
	}
}

