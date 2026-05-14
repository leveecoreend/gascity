package beads_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/beads/beadstest"
)

func TestBeadsLibStoreConformance(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skipf("dolt not found: %v", err)
	}
	store := newTestBeadsLibStore(t)
	factory := func() beads.Store {
		resetBeadsLibStore(t, store)
		return store
	}
	beadstest.RunStoreTests(t, factory)
	beadstest.RunMetadataTests(t, factory)
}

func newTestBeadsLibStore(t *testing.T) *beads.BeadsLibStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	beadsDir := filepath.Join(t.TempDir(), ".beads")
	env := append(os.Environ(),
		"BEADS_DIR="+beadsDir,
		"BEADS_DOLT_AUTO_START=1",
	)
	initCmd := exec.CommandContext(ctx, "bd", "init", "--server", "-p", "lib", "--skip-hooks")
	initCmd.Dir = filepath.Dir(beadsDir)
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init --server: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopCmd := exec.CommandContext(stopCtx, "bd", "dolt", "stop")
		stopCmd.Dir = filepath.Dir(beadsDir)
		stopCmd.Env = env
		_ = stopCmd.Run()
	})
	store, err := beads.NewBeadsLibStore(filepath.Dir(beadsDir), "lib")
	if err != nil {
		t.Fatalf("open beads lib store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Shutdown(); err != nil {
			t.Errorf("close beads lib store: %v", err)
		}
	})
	return store
}

func resetBeadsLibStore(t *testing.T, store beads.Store) {
	t.Helper()
	items, err := store.List(beads.ListQuery{AllowScan: true, IncludeClosed: true})
	if err != nil {
		t.Fatalf("list before reset: %v", err)
	}
	for _, item := range items {
		if item.Status != "closed" {
			if err := store.Close(item.ID); err != nil {
				t.Fatalf("close %s before reset: %v", item.ID, err)
			}
		}
		if err := store.Delete(item.ID); err != nil {
			t.Fatalf("delete %s before reset: %v", item.ID, err)
		}
	}
}
