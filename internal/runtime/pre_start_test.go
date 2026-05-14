package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadStartGateEnvFileParsesEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte("GC_BEAD_ID=bd-1\nGC_ACTIVE_WORK_STATUS=claimed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, ok, err := ReadStartGateEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if env["GC_BEAD_ID"] != "bd-1" {
		t.Fatalf("env = %#v, want GC_BEAD_ID bd-1", env)
	}
	if env["GC_ACTIVE_WORK_STATUS"] != "claimed" {
		t.Fatalf("env = %#v, want GC_ACTIVE_WORK_STATUS claimed", env)
	}
}

func TestReadStartGateEnvFileRejectsInvalidKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte("BAD-NAME=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ReadStartGateEnvFile(path); err == nil {
		t.Fatal("ReadStartGateEnvFile succeeded, want invalid key error")
	}
}

func TestApplyStartGateEnvRejectsInvalidValue(t *testing.T) {
	env := map[string]string{}
	err := ApplyStartGateEnv(env, map[string]string{
		"GC_ACTIVE_WORK_STATUS": "claimed\x00extra",
	})
	if err == nil {
		t.Fatal("ApplyStartGateEnv succeeded, want invalid value error")
	}
	if len(env) != 0 {
		t.Fatalf("env = %#v, want no partial updates", env)
	}
}

func TestApplyStartGateEnvAppliesGenericMap(t *testing.T) {
	env := map[string]string{"GC_BEAD_ID": "old"}
	err := ApplyStartGateEnv(env, map[string]string{
		"GC_BEAD_ID":            "bd-2",
		"GC_ACTIVE_WORK_STATUS": "claimed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["GC_BEAD_ID"] != "bd-2" {
		t.Fatalf("GC_BEAD_ID = %q, want bd-2", env["GC_BEAD_ID"])
	}
	if env["GC_ACTIVE_WORK_STATUS"] != "claimed" {
		t.Fatalf("GC_ACTIVE_WORK_STATUS = %q, want claimed", env["GC_ACTIVE_WORK_STATUS"])
	}
}

func TestWithStartGateEnvPreservesUnderlyingErrorAndEnv(t *testing.T) {
	base := errors.New("provider startup failed")
	err := WithStartGateEnv(base, map[string]string{
		"GC_BEAD_ID":            "bd-1",
		"GC_ACTIVE_WORK_STATUS": "claimed",
	})

	if !errors.Is(err, base) {
		t.Fatalf("errors.Is(err, base) = false; err=%v", err)
	}
	env := StartGateErrorEnv(err)
	if env["GC_BEAD_ID"] != "bd-1" {
		t.Fatalf("StartGateErrorEnv = %#v, want GC_BEAD_ID bd-1", env)
	}
	if env["GC_ACTIVE_WORK_STATUS"] != "claimed" {
		t.Fatalf("StartGateErrorEnv = %#v, want GC_ACTIVE_WORK_STATUS claimed", env)
	}
	env["GC_BEAD_ID"] = "mutated"
	if got := StartGateErrorEnv(err)["GC_BEAD_ID"]; got != "bd-1" {
		t.Fatalf("StartGateErrorEnv returned mutable internal map, got %q", got)
	}
}

func TestWithStartGateEnvIgnoresInvalidEnv(t *testing.T) {
	base := errors.New("provider startup failed")
	err := WithStartGateEnv(base, map[string]string{
		"BAD-NAME": "value",
	})
	if !errors.Is(err, base) {
		t.Fatalf("WithStartGateEnv returned %#v, want original error", err)
	}
	if env := StartGateErrorEnv(err); len(env) != 0 {
		t.Fatalf("StartGateErrorEnv = %#v, want empty", env)
	}
}
