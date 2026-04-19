package main

import (
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

func TestUsesGastownPackDetectsPackV2Imports(t *testing.T) {
	cfg := &config.City{
		Imports: map[string]config.Import{
			"gastown": {Source: ".gc/system/packs/gastown"},
		},
	}
	if !usesGastownPack(cfg) {
		t.Fatal("usesGastownPack = false, want true for root pack import")
	}
}

func TestUsesGastownPackDetectsDefaultRigImports(t *testing.T) {
	cfg := &config.City{
		DefaultRigImports: map[string]config.Import{
			"gastown": {Source: "packs/gastown"},
		},
	}
	if !usesGastownPack(cfg) {
		t.Fatal("usesGastownPack = false, want true for default-rig import")
	}
}
