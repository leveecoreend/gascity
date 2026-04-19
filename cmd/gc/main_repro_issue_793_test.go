package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

func TestMainRepro_Issue793_ScaleCheckTemplateExpansion(t *testing.T) {
	t.Setenv("GC_BEADS", "bd")

	cityPath := t.TempDir()
	rigPath := filepath.Join(cityPath, "demo")
	if err := os.MkdirAll(rigPath, 0o755); err != nil {
		t.Fatal(err)
	}

	checkCmd := `sh -c 'test "$1" = "demo/worker" && printf 1 || printf 0' -- "{{.Rig}}/worker"`
	cfg := &config.City{
		Rigs: []config.Rig{{
			Name: "demo",
			Path: rigPath,
		}},
		Agents: []config.Agent{{
			Name:              "worker",
			Dir:               "demo",
			MinActiveSessions: intPtr(0),
			MaxActiveSessions: intPtr(5),
			ScaleCheck:        checkCmd,
		}},
	}

	desired := buildDesiredState("test-city", cityPath, time.Now().UTC(), cfg, runtime.NewFake(), nil, io.Discard)
	workerSlots := 0
	for _, tp := range desired.State {
		if tp.TemplateName == "demo/worker" {
			workerSlots++
		}
	}
	if workerSlots != 1 {
		t.Fatalf("worker desired slots = %d, want 1 (scale_check should receive expanded demo/worker argument)", workerSlots)
	}
}
