package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

func ensureInitArtifacts(cityPath string, cfg *config.City, stderr io.Writer, commandName string) {
	if commandName == "" {
		commandName = "gc start"
	}
	if code := installClaudeHooks(fsys.OSFS{}, cityPath, stderr); code != 0 {
		fmt.Fprintf(stderr, "%s: installing claude hooks: exit %d\n", commandName, code) //nolint:errcheck // best-effort stderr
	}
	if cfg != nil && usesGastownPack(cfg) {
		if err := MaterializeGastownPacks(cityPath); err != nil {
			fmt.Fprintf(stderr, "%s: materializing gastown packs: %v\n", commandName, err) //nolint:errcheck // best-effort stderr
		}
	}
}

func usesGastownPack(cfg *config.City) bool {
	for _, include := range append(cfg.Workspace.Includes, cfg.Workspace.DefaultRigIncludes...) {
		if isGastownPackSource(include) {
			return true
		}
	}
	for _, imp := range cfg.Imports {
		if isGastownPackSource(imp.Source) {
			return true
		}
	}
	for _, imp := range cfg.DefaultRigImports {
		if isGastownPackSource(imp.Source) {
			return true
		}
	}
	return false
}

func isGastownPackSource(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	clean := filepath.Clean(source)
	if clean == filepath.Clean("packs/gastown") || clean == filepath.Clean(".gc/system/packs/gastown") {
		return true
	}
	suffix := filepath.Join("packs", "gastown")
	return strings.HasSuffix(clean, suffix)
}
