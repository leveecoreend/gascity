package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoImportMigrateShowsDoctorGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := doImportMigrate(true, &stdout, &stderr); code != 1 {
		t.Fatalf("doImportMigrate(dry-run) = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"gc import migrate has been retired as a PackV1 migration path.",
		"Run `gc doctor` to inventory legacy PackV1 surfaces and current PackV2 requirements.",
		"Run `gc doctor --fix` only for safe mechanical remediation; PackV1 layouts are no longer upgraded in place.",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
}
