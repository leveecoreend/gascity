package hooks

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

func TestSupportedProviders(t *testing.T) {
	got := SupportedProviders()
	if len(got) != 7 {
		t.Fatalf("SupportedProviders() = %v, want 7 entries", got)
	}
	want := map[string]bool{"claude": true, "gemini": true, "opencode": true, "copilot": true, "cursor": true, "pi": true, "omp": true}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected provider %q", p)
		}
	}
}

func TestValidateAcceptsSupported(t *testing.T) {
	if err := Validate([]string{"claude", "gemini"}); err != nil {
		t.Errorf("Validate([claude gemini]) = %v, want nil", err)
	}
}

func TestValidateRejectsUnsupported(t *testing.T) {
	err := Validate([]string{"claude", "codex", "auggie", "bogus"})
	if err == nil {
		t.Fatal("Validate should reject codex, auggie, and bogus")
	}
	if !strings.Contains(err.Error(), "codex (no hook mechanism)") {
		t.Errorf("error should mention codex: %v", err)
	}
	if !strings.Contains(err.Error(), "auggie (no hook mechanism)") {
		t.Errorf("error should mention auggie: %v", err)
	}
	if !strings.Contains(err.Error(), "bogus (unknown)") {
		t.Errorf("error should mention bogus: %v", err)
	}
}

func TestValidateEmpty(t *testing.T) {
	if err := Validate(nil); err != nil {
		t.Errorf("Validate(nil) = %v, want nil", err)
	}
}

func TestInstallClaude(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"claude"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/city/.gc/settings.json"]
	if !ok {
		t.Fatal("expected /city/.gc/settings.json to be written")
	}
	s := string(data)
	if !strings.Contains(s, "SessionStart") {
		t.Error("claude settings should contain SessionStart hook")
	}
	if !strings.Contains(s, "gc prime") {
		t.Error("claude settings should contain gc prime")
	}
	if !strings.Contains(s, `"skipDangerousModePermissionPrompt": true`) {
		t.Error("claude settings should contain skipDangerousModePermissionPrompt")
	}
	if !strings.Contains(s, `"editorMode": "normal"`) {
		t.Error("claude settings should contain editorMode")
	}
	if !strings.Contains(s, `$HOME/go/bin`) {
		t.Error("claude hook commands should include PATH export")
	}
}

func TestInstallGemini(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"gemini"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.gemini/settings.json"]
	if !ok {
		t.Fatal("expected /work/.gemini/settings.json to be written")
	}
	if !strings.Contains(string(data), "PreCompress") {
		t.Error("gemini settings should contain PreCompress hook")
	}
	if !strings.Contains(string(data), "BeforeAgent") {
		t.Error("gemini settings should contain BeforeAgent hook")
	}
}

func TestInstallOpenCode(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"opencode"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.opencode/plugins/gascity.js"]
	if !ok {
		t.Fatal("expected /work/.opencode/plugins/gascity.js to be written")
	}
	if !strings.Contains(string(data), "gc prime") {
		t.Error("opencode plugin should contain gc prime")
	}
}

func TestInstallCopilot(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"copilot"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.github/copilot-instructions.md"]
	if !ok {
		t.Fatal("expected /work/.github/copilot-instructions.md to be written")
	}
	if !strings.Contains(string(data), "gc prime") {
		t.Error("copilot instructions should contain gc prime")
	}
}

func TestInstallMultipleProviders(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"claude", "gemini", "copilot"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, ok := fs.Files["/city/.gc/settings.json"]; !ok {
		t.Error("missing claude settings")
	}
	if _, ok := fs.Files["/work/.gemini/settings.json"]; !ok {
		t.Error("missing gemini settings")
	}
	if _, ok := fs.Files["/work/.github/copilot-instructions.md"]; !ok {
		t.Error("missing copilot instructions")
	}
}

func TestInstallIdempotent(t *testing.T) {
	fs := fsys.NewFake()
	// Pre-populate with custom content.
	fs.Files["/city/.gc/settings.json"] = []byte(`{"custom": true}`)

	err := Install(fs, "/city", "/work", []string{"claude"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Should not overwrite existing file.
	got := string(fs.Files["/city/.gc/settings.json"])
	if got != `{"custom": true}` {
		t.Errorf("Install overwrote existing file: got %q", got)
	}
}

func TestInstallUnknownProvider(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"bogus"})
	if err == nil {
		t.Fatal("Install should reject unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported: %v", err)
	}
}

func TestInstallCursor(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"cursor"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.cursor/hooks.json"]
	if !ok {
		t.Fatal("expected /work/.cursor/hooks.json to be written")
	}
	if !strings.Contains(string(data), "sessionStart") {
		t.Error("cursor hooks should contain sessionStart")
	}
	if !strings.Contains(string(data), "gc prime") {
		t.Error("cursor hooks should contain gc prime")
	}
	if !strings.Contains(string(data), "gc mail check --inject") {
		t.Error("cursor hooks should contain gc mail check --inject")
	}
}

func TestInstallPi(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"pi"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.pi/extensions/gc-hooks.js"]
	if !ok {
		t.Fatal("expected /work/.pi/extensions/gc-hooks.js to be written")
	}
	s := string(data)
	if !strings.Contains(s, "gc prime") {
		t.Error("pi hooks should contain gc prime")
	}
	if !strings.Contains(s, "gc hook --inject") {
		t.Error("pi hooks should contain gc hook --inject")
	}
}

func TestInstallOmp(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", []string{"omp"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, ok := fs.Files["/work/.omp/hooks/gc-hook.ts"]
	if !ok {
		t.Fatal("expected /work/.omp/hooks/gc-hook.ts to be written")
	}
	s := string(data)
	if !strings.Contains(s, "gc prime") {
		t.Error("omp hooks should contain gc prime")
	}
	if !strings.Contains(s, "gc hook --inject") {
		t.Error("omp hooks should contain gc hook --inject")
	}
}

// TestSupportsHooksSyncWithProviderSpec verifies that the hooks supported/unsupported
// lists stay in sync with ProviderSpec.SupportsHooks across all builtin providers.
func TestSupportsHooksSyncWithProviderSpec(t *testing.T) {
	sup := make(map[string]bool, len(SupportedProviders()))
	for _, p := range SupportedProviders() {
		sup[p] = true
	}

	providers := config.BuiltinProviders()
	for name, spec := range providers {
		if spec.SupportsHooks && !sup[name] {
			t.Errorf("provider %q has SupportsHooks=true but is not in hooks.SupportedProviders()", name)
		}
		if !spec.SupportsHooks && sup[name] {
			t.Errorf("provider %q is in hooks.SupportedProviders() but has SupportsHooks=false", name)
		}
	}
}

func TestInstallEmpty(t *testing.T) {
	fs := fsys.NewFake()
	err := Install(fs, "/city", "/work", nil)
	if err != nil {
		t.Fatalf("Install(nil) = %v, want nil", err)
	}
}
