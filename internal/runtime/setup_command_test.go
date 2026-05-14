package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRunStartGateDoesNotCarryInvalidEnv(t *testing.T) {
	cfg := Config{
		StartGate: "claim-work",
		Env:       map[string]string{},
	}

	_, err := RunStartGate(context.Background(), cfg, time.Second, func(_ context.Context, _ string, env map[string]string, _ time.Duration) error {
		return os.WriteFile(env[StartGateEnv], []byte("BAD-NAME=value\n"), 0o600)
	})
	if err == nil {
		t.Fatal("RunStartGate succeeded, want validation failure")
	}
	if env := StartGateErrorEnv(err); len(env) != 0 {
		t.Fatalf("StartGateErrorEnv = %#v, want no env for invalid map; err=%v", env, err)
	}
}

func TestRunStartGateCarriesOnlyReturnedEnv(t *testing.T) {
	cfg := Config{
		StartGate: "claim-work",
		Env: map[string]string{
			"SECRET_TOKEN": "do-not-carry",
		},
	}

	_, err := RunStartGate(context.Background(), cfg, time.Second, func(_ context.Context, _ string, env map[string]string, _ time.Duration) error {
		if writeErr := os.WriteFile(env[StartGateEnv], []byte("GC_BEAD_ID=bd-1\nGC_ACTIVE_WORK_STATUS=claimed\n"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return errors.New("provider start failed")
	})
	if err == nil {
		t.Fatal("RunStartGate succeeded, want command failure")
	}
	env := StartGateErrorEnv(err)
	if env["GC_BEAD_ID"] != "bd-1" || env["GC_ACTIVE_WORK_STATUS"] != "claimed" {
		t.Fatalf("StartGateErrorEnv = %#v, want returned env map", env)
	}
	if _, ok := env["SECRET_TOKEN"]; ok {
		t.Fatalf("StartGateErrorEnv = %#v, should not include ambient config env", env)
	}
}

func TestRunStartGateJoinsCommandAndEnvReadErrors(t *testing.T) {
	cmdErr := errors.New("claim store unavailable")

	_, err := RunStartGate(context.Background(), Config{StartGate: "claim-work"}, time.Second, func(_ context.Context, _ string, env map[string]string, _ time.Duration) error {
		if writeErr := os.WriteFile(env[StartGateEnv], []byte("not-an-assignment\n"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return cmdErr
	})
	if err == nil {
		t.Fatal("RunStartGate succeeded, want command/read failure")
	}
	if !errors.Is(err, cmdErr) {
		t.Fatalf("errors.Is(err, cmdErr) = false; err=%v", err)
	}
	if !errors.Is(err, cmdErr) || err.Error() == cmdErr.Error() {
		t.Fatalf("error = %v, want joined command and env parse context", err)
	}
}
