package db

import "testing"

func TestRewritePlaceholders_PostgresBasic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single placeholder", `SELECT * FROM t WHERE id = ?`, `SELECT * FROM t WHERE id = $1`},
		{"two placeholders", `INSERT INTO t VALUES (?, ?)`, `INSERT INTO t VALUES ($1, $2)`},
		{
			"upsert with mix",
			`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			`INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, $3) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		},
		{
			"placeholder in string is NOT rewritten",
			`SELECT * FROM t WHERE name = '?' AND id = ?`,
			`SELECT * FROM t WHERE name = '?' AND id = $1`,
		},
		{
			"escaped quote inside string",
			`SELECT 'a''b?c' AS x WHERE id = ?`,
			`SELECT 'a''b?c' AS x WHERE id = $1`,
		},
		{
			"placeholder in line comment is NOT rewritten",
			"-- user's id is ?\nSELECT * FROM t WHERE id = ?",
			"-- user's id is ?\nSELECT * FROM t WHERE id = $1",
		},
		{"no placeholders", `SELECT 1`, `SELECT 1`},
		{"only comments", `-- hello\n-- world\n`, `-- hello\n-- world\n`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewritePlaceholders(DriverPostgres, tc.in)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestRewritePlaceholders_SQLitePassthrough(t *testing.T) {
	// On SQLite the helper is a no-op — input string returned verbatim.
	cases := []string{
		`SELECT * FROM t WHERE id = ?`,
		`INSERT INTO t VALUES (?, ?, ?)`,
		`SELECT 'a''b?c' AS x WHERE id = ?`,
	}
	for _, in := range cases {
		got := rewritePlaceholders(DriverSQLite, in)
		if got != in {
			t.Errorf("SQLite passthrough mutated query:\ngot:  %s\nwant: %s", got, in)
		}
	}
	// Empty driver string defaults to sqlite branch (no rewrite).
	if rewritePlaceholders("", "SELECT ?") != "SELECT ?" {
		t.Errorf("empty driver should pass through")
	}
}

func TestIsPostgres(t *testing.T) {
	if !IsPostgres("postgres") {
		t.Error(`"postgres" should be Postgres`)
	}
	for _, d := range []string{"sqlite", "", "mysql", "Postgres", "POSTGRES"} {
		if IsPostgres(d) {
			t.Errorf("%q should NOT be Postgres", d)
		}
	}
}
