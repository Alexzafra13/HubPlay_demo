package handlers

import (
	"net/http"
	"time"
)

// DisableWriteDeadline clears the per-request write deadline so a
// long-lived handler (HLS streaming, SSE, big file download, peer
// stream proxy) can write for an indefinite period without the
// HTTP server killing the connection mid-stream.
//
// El default global en `cmd/hubplay/main.go` setea
// `WriteTimeout: 30s` (cierre del olor Q de la auditoría
// 2026-05-14): el 95 % de las rutas — JSON CRUD bajo /api/v1/* —
// no necesitan más, y heredaban antes el WriteTimeout: 0 global
// que dejaba abiertas goroutines de clientes lentos. Los ~10
// handlers que SÍ son streaming opt-out llamando a este helper al
// principio de su cuerpo, antes de escribir headers.
//
// La llamada es no-op si el `http.ResponseWriter` subyacente no
// implementa `SetWriteDeadline` (ResponseController soporta el
// fallback automáticamente desde Go 1.20). Devolvemos cualquier
// error para que el handler decida si seguir o no — la mayoría
// pueden simplemente ignorarlo y log a debug porque el resultado
// es "el WriteTimeout global aplica como fallback razonable".
//
// Patrón de uso:
//
//	func (h *StreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
//	    _ = DisableWriteDeadline(w) // opt-out del 30s default; segments pueden tardar más
//	    // ... el cuerpo del handler
//	}
//
// El nombre exportado para que cualquier package del mismo módulo
// que añada un endpoint streaming pueda invocarlo sin duplicar el
// helper.
func DisableWriteDeadline(w http.ResponseWriter) error {
	// time.Time{} (cero) le dice al servidor "no deadline".
	return http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
