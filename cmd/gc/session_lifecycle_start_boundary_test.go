package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
)

func TestExecutePreparedStartWaveUsesWorkerBoundaryForKnownSession(t *testing.T) {
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	mgr := newSessionManagerWithConfig("", store, sp, nil)
	info, err := mgr.CreateBeadOnly("worker", "Worker", "claude", t.TempDir(), "claude", "", nil, sessionpkg.ProviderResume{})
	if err != nil {
		t.Fatalf("CreateBeadOnly: %v", err)
	}
	bead, err := store.Get(info.ID)
	if err != nil {
		t.Fatalf("Get bead: %v", err)
	}

	results := executePreparedStartWave(
		context.Background(),
		[]preparedStart{{
			candidate: startCandidate{
				session: &bead,
				tp:      TemplateParams{TemplateName: "worker"},
			},
			cfg: runtime.Config{
				Command: "claude --resume seeded-session",
				WorkDir: info.WorkDir,
			},
		}},
		sp,
		store,
		10*time.Second,
	)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].err != nil {
		t.Fatalf("start result err = %v, want nil", results[0].err)
	}

	got, err := mgr.Get(info.ID)
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	if got.State != sessionpkg.StateActive {
		t.Fatalf("state = %q, want %q after worker start", got.State, sessionpkg.StateActive)
	}
	updatedBead, err := store.Get(info.ID)
	if err != nil {
		t.Fatalf("Get updated bead: %v", err)
	}
	if updatedBead.Metadata["pending_create_claim"] != "" {
		t.Fatalf("pending_create_claim = %q, want worker start to clear claim", updatedBead.Metadata["pending_create_claim"])
	}
	if !sp.IsRunning(info.SessionName) {
		t.Fatal("session should be running after prepared start")
	}
}

func TestStartPreparedStartCandidateRejectsRuntimeOnlyTarget(t *testing.T) {
	sp := runtime.NewFake()
	sessionBead := &beads.Bead{
		Metadata: map[string]string{
			"session_name": "legacy-runtime-only",
		},
	}

	usedWorker, err := startPreparedStartCandidate(
		context.Background(),
		preparedStart{
			candidate: startCandidate{
				session: sessionBead,
				tp:      TemplateParams{TemplateName: "worker"},
			},
			cfg: runtime.Config{
				Command: "claude --resume seeded",
				WorkDir: t.TempDir(),
			},
		},
		"",
		nil,
		sp,
		nil,
	)
	if err != nil {
		if usedWorker {
			t.Fatal("usedWorker = true, want false for runtime-only start rejection")
		}
		if !strings.Contains(err.Error(), "start requires a bead-backed session") {
			t.Fatalf("err = %v, want bead-backed session rejection", err)
		}
		return
	}
	t.Fatal("startPreparedStartCandidate succeeded for runtime-only target")
}
