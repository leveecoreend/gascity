package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/cityinit"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/supervisor"
)

func TestValidateInitRequest(t *testing.T) {
	absDir := filepath.Join(t.TempDir(), "city")
	tests := []struct {
		name         string
		req          cityinit.InitRequest
		wantErr      error
		wantContains string
	}{
		{
			name:    "missing dir",
			req:     cityinit.InitRequest{Provider: "codex"},
			wantErr: cityinit.ErrInvalidProvider,
		},
		{
			name:         "relative dir",
			req:          cityinit.InitRequest{Dir: "relative", Provider: "codex"},
			wantContains: "dir must be absolute",
		},
		{
			name:    "missing provider and start command",
			req:     cityinit.InitRequest{Dir: absDir},
			wantErr: cityinit.ErrInvalidProvider,
		},
		{
			name:    "unknown provider",
			req:     cityinit.InitRequest{Dir: absDir, Provider: "not-a-provider"},
			wantErr: cityinit.ErrInvalidProvider,
		},
		{
			name:    "bad bootstrap profile",
			req:     cityinit.InitRequest{Dir: absDir, Provider: "codex", BootstrapProfile: "moon-base"},
			wantErr: cityinit.ErrInvalidBootstrapProfile,
		},
		{
			name:    "builtin provider",
			req:     cityinit.InitRequest{Dir: absDir, Provider: "codex"},
			wantErr: nil,
		},
		{
			name:    "custom start command",
			req:     cityinit.InitRequest{Dir: absDir, StartCommand: "custom-agent"},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInitRequest(&tc.req)
			if tc.wantErr == nil {
				if tc.wantContains != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantContains) {
						t.Fatalf("validateInitRequest() error = %v, want message containing %q", err, tc.wantContains)
					}
					return
				}
				if err != nil {
					t.Fatalf("validateInitRequest() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateInitRequest() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestLocalInitializerScaffoldCreatesCityRegistersAndEmitsCreated(t *testing.T) {
	t.Setenv("GC_HOME", t.TempDir())
	cityPath := filepath.Join(t.TempDir(), "api-city")

	result, err := localInitializer{}.Scaffold(context.Background(), cityinit.InitRequest{
		Dir:              cityPath,
		Provider:         "codex",
		BootstrapProfile: bootstrapProfileSingleHostCompat,
		NameOverride:     "api-city",
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if result.CityName != "api-city" || result.CityPath != cityPath || result.ProviderUsed != "codex" {
		t.Fatalf("Scaffold result = %+v, want api-city/%s/codex", result, cityPath)
	}
	if _, err := os.Stat(filepath.Join(cityPath, "city.toml")); err != nil {
		t.Fatalf("city.toml missing after Scaffold: %v", err)
	}

	reg := supervisor.NewRegistry(supervisor.RegistryPath())
	entries, err := reg.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("registry entries = %+v, want one", entries)
	}
	if entries[0].EffectiveName() != "api-city" {
		t.Fatalf("registry effective name = %q, want api-city", entries[0].EffectiveName())
	}
	assertSameTestPath(t, entries[0].Path, cityPath)

	evts, err := events.ReadFiltered(filepath.Join(cityPath, ".gc", "events.jsonl"), events.Filter{Type: events.CityCreated})
	if err != nil {
		t.Fatalf("ReadFiltered city.created: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("city.created events = %d, want 1: %+v", len(evts), evts)
	}
	var payload api.CityCreatedPayload
	if err := json.Unmarshal(evts[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal city.created payload: %v", err)
	}
	if payload.Name != "api-city" {
		t.Fatalf("payload name = %q, want api-city", payload.Name)
	}
	assertSameTestPath(t, payload.Path, cityPath)

	_, err = localInitializer{}.Scaffold(context.Background(), cityinit.InitRequest{
		Dir:      cityPath,
		Provider: "codex",
	})
	if !errors.Is(err, cityinit.ErrAlreadyInitialized) {
		t.Fatalf("second Scaffold error = %v, want ErrAlreadyInitialized", err)
	}
}

func TestLocalInitializerInitScaffoldsAndFinalizes(t *testing.T) {
	cityPath := filepath.Join(t.TempDir(), "init-city")

	result, err := localInitializer{}.Init(context.Background(), cityinit.InitRequest{
		Dir:                   cityPath,
		StartCommand:          "true",
		NameOverride:          "init-city",
		SkipProviderReadiness: true,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.CityName != "init-city" || result.CityPath != cityPath {
		t.Fatalf("Init result = %+v, want init-city/%s", result, cityPath)
	}
	if _, err := os.Stat(filepath.Join(cityPath, ".gc")); err != nil {
		t.Fatalf(".gc missing after Init finalization: %v", err)
	}

	_, err = localInitializer{}.Init(context.Background(), cityinit.InitRequest{
		Dir:          cityPath,
		StartCommand: "true",
	})
	if !errors.Is(err, cityinit.ErrAlreadyInitialized) {
		t.Fatalf("second Init error = %v, want ErrAlreadyInitialized", err)
	}
}

func TestLocalInitializerUnregisterRemovesRegistryAndEmitsEvent(t *testing.T) {
	t.Setenv("GC_HOME", t.TempDir())
	cityPath := filepath.Join(t.TempDir(), "bright-lights")
	if err := os.MkdirAll(filepath.Join(cityPath, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := supervisor.NewRegistry(supervisor.RegistryPath())
	if err := reg.Register(cityPath, "bright-lights"); err != nil {
		t.Fatal(err)
	}

	result, err := localInitializer{}.Unregister(context.Background(), cityinit.UnregisterRequest{
		CityName: " bright-lights ",
	})
	if err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if result.CityName != "bright-lights" {
		t.Fatalf("CityName = %q, want bright-lights", result.CityName)
	}
	assertSameTestPath(t, result.CityPath, cityPath)

	entries, err := reg.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("registry entries after unregister = %+v, want empty", entries)
	}

	evts, err := events.ReadFiltered(filepath.Join(cityPath, ".gc", "events.jsonl"), events.Filter{Type: events.CityUnregisterRequested})
	if err != nil {
		t.Fatalf("ReadFiltered: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("unregister_requested events = %d, want 1: %+v", len(evts), evts)
	}
	if evts[0].Actor != "gc" || evts[0].Subject != "bright-lights" {
		t.Fatalf("event actor/subject = %q/%q, want gc/bright-lights", evts[0].Actor, evts[0].Subject)
	}
	var payload api.CityUnregisterRequestedPayload
	if err := json.Unmarshal(evts[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Name != "bright-lights" {
		t.Fatalf("payload name = %q, want bright-lights", payload.Name)
	}
	assertSameTestPath(t, payload.Path, cityPath)
}

func TestLocalInitializerUnregisterMissingCity(t *testing.T) {
	t.Setenv("GC_HOME", t.TempDir())

	_, err := localInitializer{}.Unregister(context.Background(), cityinit.UnregisterRequest{CityName: "missing"})
	if !errors.Is(err, cityinit.ErrNotRegistered) {
		t.Fatalf("Unregister missing error = %v, want ErrNotRegistered", err)
	}

	_, err = localInitializer{}.Unregister(context.Background(), cityinit.UnregisterRequest{})
	if !errors.Is(err, cityinit.ErrNotRegistered) {
		t.Fatalf("Unregister blank error = %v, want ErrNotRegistered", err)
	}
}
