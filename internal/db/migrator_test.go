package db

import (
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"users", `"users"`},
		{`weird"name`, `"weird""name"`},
		{"", `""`},
	}
	for _, c := range cases {
		if got := quoteIdent(c.in); got != c.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildInsertSQL(t *testing.T) {
	got := buildInsertSQL("items", []string{"id", "title", "year"})
	want := `INSERT INTO "items" ("id", "title", "year") VALUES ($1, $2, $3)`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFilterOut(t *testing.T) {
	got := filterOut([]string{"a", "b", "c"}, "b")
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("filterOut = %v", got)
	}
}

func TestNormalizeValue_Boolean(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		pgType string
		want   any
	}{
		{"int64 1 → true", int64(1), "boolean", true},
		{"int64 0 → false", int64(0), "boolean", false},
		{"bool passthrough", true, "boolean", true},
		{"nil passthrough", nil, "boolean", nil},
		{"non-boolean type unchanged", int64(42), "integer", int64(42)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeValue(c.in, c.pgType); got != c.want {
				t.Errorf("normalizeValue = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseSQLiteTime(t *testing.T) {
	// modernc format with monotonic clock suffix.
	if v := parseSQLiteTime("2026-01-15 12:34:56.789 +0000 UTC"); v == "2026-01-15 12:34:56.789 +0000 UTC" {
		t.Errorf("modernc-shape string was not parsed: %v", v)
	}
	// RFC3339 — common when the value came in via JSON or a sqlc
	// path that already formatted it.
	if v := parseSQLiteTime("2026-01-15T12:34:56Z"); v == "2026-01-15T12:34:56Z" {
		t.Errorf("RFC3339 string was not parsed: %v", v)
	}
	// Garbage in → garbage out (returned verbatim so the caller
	// can produce a useful error pointing at the row).
	if v := parseSQLiteTime("not a time"); v != "not a time" {
		t.Errorf("garbage should pass through, got %v", v)
	}
}

func TestRedactDSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url form with password",
			in:   "postgres://alice:secret@host:5432/db?sslmode=require",
			want: "postgres://alice:***@host:5432/db?sslmode=require",
		},
		{
			name: "postgresql scheme",
			in:   "postgresql://bob:hunter2@host/db",
			want: "postgresql://bob:***@host/db",
		},
		{
			name: "url without password is unchanged",
			in:   "postgres://alice@host:5432/db",
			want: "postgres://alice@host:5432/db",
		},
		{
			name: "libpq key=value form",
			in:   "host=h user=u password=topsecret dbname=d sslmode=disable",
			want: "host=h user=u password=*** dbname=d sslmode=disable",
		},
		{
			name: "sqlite path passes through",
			in:   "/data/hubplay.db",
			want: "/data/hubplay.db",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactDSN(c.in); got != c.want {
				t.Errorf("RedactDSN(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
