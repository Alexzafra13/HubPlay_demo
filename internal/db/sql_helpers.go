package db

import "strings"

// sqlPlaceholders devuelve `"?,?,?,..."` con n placeholders, sin coma
// final. Devuelve `""` cuando n <= 0 (el caller debe gatear el caso
// "lista vacía" antes de embeber el resultado en una cláusula IN —
// `IN ()` es syntax error SQL).
//
// Reemplaza el patrón anti-idiomático `placeholders += ",?"` en bucle
// (audit olor F14-12-a, "+=" para concat). Performance OK para los
// tamaños actuales pero olor visible.
func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
