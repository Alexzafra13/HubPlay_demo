package db_test

// sqlc drift test — regenerates the *.sql.go files into a temp dir using
// the pinned SQLC_VERSION and asserts byte-identical output against the
// committed files. Three regression classes this catches:
//
//   1. Someone edited a .sql query but forgot to run `make sqlc`.
//   2. Someone introduced a parser-hostile pattern in a query (multi-byte
//      chars in comments — em-dashes / accented letters / backticks
//      shift sqlc's UTF-8 byte counting and silently truncate subsequent
//      queries; positional `?` placeholders nested inside `NOT (...)`
//      get dropped from the generated Params struct).
//   3. Someone bumped SQLC_VERSION in the Makefile without re-baselining.
//
// Why a Go test rather than a CI shell step: keeping the check in
// `go test ./...` means it runs in every contributor's normal workflow
// (the same place the rest of the regression suite lives) and the
// failure message can be as detailed as we want.
//
// The test is skipped when sqlc isn't on PATH so a contributor without
// the tool installed isn't blocked on local `go test`. CI installs sqlc
// (the Makefile sqlc-install target does it idempotently) so the check
// is enforced where it matters.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSQLC_GeneratedFilesMatchQueries(t *testing.T) {
	sqlcBin, err := exec.LookPath("sqlc")
	if err != nil {
		t.Skip("sqlc not on PATH; install with `make sqlc-install` to enable this drift check locally. CI runs it.")
	}

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// sqlc resolves all paths in its config (queries, schema, out)
	// relative to the config file's parent directory and ignores
	// absolute paths (treats them as relative-to-cwd anyway). So we
	// drop both the config and the scratch out dir inside repoRoot,
	// using gitignored `.sqlc-drift-*` names, and clean up after.
	scratchOut := ".sqlc-drift-out"
	scratchOutAbs := filepath.Join(repoRoot, scratchOut)
	defer os.RemoveAll(scratchOutAbs) //nolint:errcheck

	scratchCfgPath := filepath.Join(repoRoot, ".sqlc-drift-test.yaml")
	scratchCfg := `version: "2"
sql:
  - engine: "sqlite"
    queries: "internal/db/queries/"
    schema: "migrations/sqlite/"
    gen:
      go:
        package: "sqlc"
        out: "` + scratchOut + `"
        emit_interface: true
        emit_json_tags: true
        emit_empty_slices: true
        overrides:
          - column: "*.id"
            go_type: "string"
          - column: "*.created_at"
            go_type: "time.Time"
          - column: "*.updated_at"
            go_type: "time.Time"
`
	if err := os.WriteFile(scratchCfgPath, []byte(scratchCfg), 0o644); err != nil {
		t.Fatalf("write scratch sqlc.yaml: %v", err)
	}
	defer os.Remove(scratchCfgPath) //nolint:errcheck

	cmd := exec.Command(sqlcBin, "generate", "--file", scratchCfgPath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sqlc generate failed: %v\n%s", err, out)
	}

	// Walk scratch output, compare each file against the committed
	// counterpart. Differences either way (extra file, missing file,
	// content drift) → fail with a focused message.
	committedRoot := filepath.Join(repoRoot, "internal", "db", "sqlc")

	var drifted []string
	err = filepath.Walk(scratchOutAbs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(scratchOutAbs, path)
		fresh, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		committed, cerr := os.ReadFile(filepath.Join(committedRoot, rel))
		if cerr != nil {
			drifted = append(drifted, rel+" (missing in committed tree)")
			return nil
		}
		if !bytes.Equal(fresh, committed) {
			drifted = append(drifted, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk scratch output: %v", err)
	}

	if len(drifted) > 0 {
		t.Fatalf(`sqlc drift detected: the committed internal/db/sqlc/*.sql.go
files do not match what 'sqlc generate' produces from the current
queries. Files that drift:

  - %s

Either:
  (a) you edited an .sql file but forgot to run 'make sqlc' and commit
      the regen, or
  (b) you introduced a query pattern that triggers a sqlc parser bug
      (multi-byte chars in comments, '?' placeholders inside NOT (...)
      clauses) -- see docs/memory/conventions.md "Regeneracion sqlc".

To inspect the drift: 'make sqlc' and 'git diff internal/db/sqlc/'`,
			strings.Join(drifted, "\n  - "))
	}
}
