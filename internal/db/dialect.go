package db

import (
	"strings"
)

// Driver name constants. Used throughout the dual-dialect repo
// constructors to pick the right SQL flavour. "sqlite" is the
// default branch — anything that isn't "postgres" lands on it.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// IsPostgres reports whether the given driver string selects the
// PostgreSQL backend. Centralised so a future rename / addition of
// driver names is a one-line change.
func IsPostgres(driver string) bool {
	return driver == DriverPostgres
}

// rewritePlaceholders converts SQLite-style `?` positional placeholders
// to PostgreSQL-style `$N` numbered placeholders. Counter increments
// once per `?` outside of string literals and SQL line comments.
//
// Used by every raw-SQL repo to keep one SQL source (with `?`) while
// running against either backend. The conversion is computed ONCE at
// repo construction time (see SettingsRepository for the canonical
// pattern), so per-query cost is zero.
//
// When driver != "postgres" the input string is returned unchanged.
//
// Edge cases handled:
//
//   - String literals: `'?'` survives intact (the parser tracks `'`
//     pairs and doesn't substitute inside them). SQL `''` is the
//     escape for an embedded single quote inside a string, also
//     handled.
//   - Line comments: `-- foo's bar ?` — the apostrophe-in-comment
//     trap doesn't toggle string state because the parser exits
//     comment mode at `\n` instead. Same gotcha caught earlier in
//     the queries-postgres conversion script.
//
// NOT handled (out of scope — we never use them):
//
//   - Block comments `/* ... */`
//   - Dollar-quoted strings `$$ ... $$` (we don't write triggers
//     by hand at the repo SQL level; goose handles those at
//     migration time)
func rewritePlaceholders(driver, query string) string {
	if !IsPostgres(driver) {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	counter := 0
	inString := false
	inComment := false
	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inComment {
			b.WriteRune(c)
			if c == '\n' {
				inComment = false
			}
			continue
		}
		if !inString && c == '-' && i+1 < len(runes) && runes[i+1] == '-' {
			inComment = true
			b.WriteRune(c)
			continue
		}
		if c == '\'' {
			// '' inside a string is the SQL escape for embedded '.
			if inString && i+1 < len(runes) && runes[i+1] == '\'' {
				b.WriteString("''")
				i++
				continue
			}
			inString = !inString
			b.WriteRune(c)
			continue
		}
		if c == '?' && !inString {
			counter++
			b.WriteRune('$')
			b.WriteString(itoa(counter))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// itoa is a tiny strconv.Itoa replacement to avoid importing
// `strconv` for one use. Counters never exceed a few dozen.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
