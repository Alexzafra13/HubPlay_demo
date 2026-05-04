package db_test

// sqlc lockdown guard regression test.
//
// The Makefile guards `make sqlc` behind HUBPLAY_REGEN_SQLC=1 because the
// committed internal/db/sqlc/*.sql.go files are hand-validated and current
// sqlc versions corrupt the regen output (em-dashes in comments + missed
// parameter detection inside NOT(...) clauses + NULL-type drift). Detailed
// rationale: docs/memory/conventions.md section "Regeneración sqlc → bloqueada".
//
// If someone removes the guard during a Makefile refactor, this test fails so
// the regression surfaces in CI rather than after a contributor accidentally
// regenerates and corrupts the output.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakefile_SQLCGuardIsInPlace(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	makefilePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "Makefile")
	body, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	makefile := string(body)

	// The sqlc target must include the env-var guard. Check for the literal
	// guard variable; brittle on purpose — if you renamed HUBPLAY_REGEN_SQLC,
	// update conventions.md too so the playbook stays accurate.
	if !strings.Contains(makefile, "HUBPLAY_REGEN_SQLC") {
		t.Fatal("Makefile no longer references HUBPLAY_REGEN_SQLC bypass var.\n" +
			"The `sqlc` target must keep the guard that aborts unless\n" +
			"HUBPLAY_REGEN_SQLC=1 is set. See docs/memory/conventions.md\n" +
			"section 'Regeneración sqlc → bloqueada' for why.")
	}

	// Sanity: the guard message must point at the conventions doc so a
	// developer hitting the abort lands on the playbook, not a dead end.
	if !strings.Contains(makefile, "conventions.md") {
		t.Error("Makefile sqlc-guard message no longer references conventions.md;\n" +
			"developers who hit the abort need a pointer to the unlock playbook.")
	}
}
