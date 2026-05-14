package storage

import (
	"database/sql"
	"strings"
)

// nullableString wraps a plain string for storage in a nullable TEXT
// column. Empty string → NULL, anything else → valid.
//
// Copia privada de la versión definida en `internal/db` para no
// exportar API trivial cruzando paquetes. La función es pura y de 4
// líneas; mantener una réplica local pesa menos que un import path.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// toTSQueryPrefix builds a Postgres `to_tsquery` payload from
// free-form user input, doing prefix matching on the LAST token.
//
// We pre-sanitise rather than trust the parser because raw user input
// like "harry+potter (2001)" would otherwise raise a syntax error. An
// empty / fully-stripped query becomes `""` which `to_tsquery` accepts
// and matches nothing — the right semantic for "the user typed only
// punctuation".
//
// Copia privada de la versión homónima en `internal/db/item_repository.go`;
// el item handler la sigue usando allí. Mantener la copia evita
// exponer el helper cross-package por una única call-site adicional.
func toTSQueryPrefix(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	parts := strings.Fields(q)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.Map(func(r rune) rune {
			switch r {
			case '&', '|', '!', '(', ')', ':', '*', '<', '>', '\'', '"', '\\', '/', '%', '?', '$', '@', '#':
				return -1
			}
			return r
		}, p)
		if clean != "" {
			tokens = append(tokens, clean)
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	tokens[len(tokens)-1] += ":*"
	return strings.Join(tokens, " & ")
}
