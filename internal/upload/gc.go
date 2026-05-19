package upload

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// GC limpia uploads huérfanos del staging dir — directorios de upload
// (un nivel por usuario, otro por uploadID) cuyo modtime es más antiguo
// que `staleAfter`.
//
// Por qué existe: si el binario cae mientras un upload está en vuelo,
// los chunks PATCHed + el .info de tusd quedan en
// `<staging>/<user>/<id>/`. La pipeline Service.Finish nunca corre,
// la cuota nunca se libera (ese path es harder de arreglar, ver más
// abajo), y el blob ocupa espacio para siempre. Operadores reales
// en self-hosted prefieren un cron que lo limpie automáticamente a
// tener que rm -rf manual.
//
// Política de borrado por seguridad:
//   - Sólo escanea exactamente DOS niveles bajo staging.Root()
//     (<user>/<upload>). Cualquier cosa fuera no se toca.
//   - Confianza en modtime: si todos los ficheros del dir tienen
//     modtime > staleAfter, lo borra. Un upload "lento" pero vivo
//     escribe periódicamente y NO cumple esta condición (tus PATCH
//     actualiza el modtime de los chunks).
//   - Idempotente: si falla a mitad, la siguiente pasada continúa.
//
// Caveat de cuota: el GC NO devuelve la cuota reservada. El user
// quedó con `upload_used_bytes` incrementado y nunca decrementado
// porque Service.Aborted/Finish no corrió. Resolverlo bien
// requeriría leer el .info de tusd (sabemos qué user) + parsear el
// estado. Lo dejo para una v2; en v1 el operador puede ajustar la
// cuota a mano si nota drift, y el GC al menos recupera el disco.
type GC struct {
	staging      *StagingDir
	logger       *slog.Logger
	interval     time.Duration
	staleAfter   time.Duration
}

// NewGC crea el garbage collector. Valores típicos:
//   - interval   = 1 * time.Hour
//   - staleAfter = 24 * time.Hour
//
// Demasiado agresivo (e.g. staleAfter=1h) puede borrar uploads
// pausados que el usuario quería retomar. Demasiado conservador
// (staleAfter=30d) deja el disco crecer durante meses entre crashes.
// 24h es el sweet spot — un upload que lleva 24h sin actividad es
// claramente abandonado, no pausado.
func NewGC(staging *StagingDir, interval, staleAfter time.Duration, logger *slog.Logger) *GC {
	return &GC{
		staging:    staging,
		logger:     logger.With("module", "upload-gc"),
		interval:   interval,
		staleAfter: staleAfter,
	}
}

// Start arranca el bucle en una goroutine. Se cancela vía ctx.Done()
// que main pasa al recibir SIGINT/SIGTERM. La primera pasada va con
// 30s de retraso para que el boot del server no compita con I/O del
// GC en el momento crítico de arranque.
func (g *GC) Start(ctx context.Context) {
	if g.staging == nil {
		g.logger.Warn("upload GC not starting — staging dir is nil")
		return
	}
	if g.interval <= 0 || g.staleAfter <= 0 {
		g.logger.Warn("upload GC disabled — non-positive interval or staleAfter")
		return
	}
	g.logger.Info("upload GC started",
		"interval", g.interval, "stale_after", g.staleAfter)

	go func() {
		// Primera pasada diferida para no competir con el boot.
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return
		}
		g.sweep(ctx)

		ticker := time.NewTicker(g.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				g.sweep(ctx)
			case <-ctx.Done():
				g.logger.Info("upload GC stopped")
				return
			}
		}
	}()
}

// sweep recorre <staging>/<user>/<upload> y borra los directorios
// huérfanos. No usa filepath.Walk para evitar recursión accidental
// (queremos exactamente 2 niveles, no más).
func (g *GC) sweep(ctx context.Context) {
	root := g.staging.Root()
	users, err := os.ReadDir(root)
	if err != nil {
		g.logger.Warn("sweep: read staging root failed", "error", err)
		return
	}

	cutoff := time.Now().Add(-g.staleAfter)
	deleted, scanned := 0, 0

	for _, userEnt := range users {
		if !userEnt.IsDir() {
			continue
		}
		userDir := filepath.Join(root, userEnt.Name())
		uploads, err := os.ReadDir(userDir)
		if err != nil {
			g.logger.Warn("sweep: read user dir failed",
				"path", userDir, "error", err)
			continue
		}

		for _, uploadEnt := range uploads {
			if ctx.Err() != nil {
				return // shutdown a media pasada — ok.
			}
			if !uploadEnt.IsDir() {
				continue
			}
			scanned++
			uploadDir := filepath.Join(userDir, uploadEnt.Name())
			stale, err := g.isStale(uploadDir, cutoff)
			if err != nil {
				g.logger.Warn("sweep: stat failed",
					"path", uploadDir, "error", err)
				continue
			}
			if !stale {
				continue
			}
			if err := os.RemoveAll(uploadDir); err != nil {
				g.logger.Warn("sweep: remove failed",
					"path", uploadDir, "error", err)
				continue
			}
			deleted++
			g.logger.Info("sweep: removed orphan upload",
				"user", userEnt.Name(), "upload", uploadEnt.Name())
		}

		// Si el dir del usuario quedó vacío tras los borrados, también
		// lo limpiamos — un usuario sin uploads no necesita estructura
		// fantasma. ReadDir + check de longitud es barato.
		if remaining, err := os.ReadDir(userDir); err == nil && len(remaining) == 0 {
			_ = os.Remove(userDir)
		}
	}

	if deleted > 0 || scanned > 0 {
		g.logger.Info("sweep complete",
			"scanned", scanned, "deleted", deleted)
	}
}

// isStale devuelve true si TODOS los ficheros dentro de `dir` tienen
// modtime <= cutoff. Un único fichero modificado recientemente
// (chunk en escritura) protege el dir entero — preferimos sobre-
// conservador a borrar un upload activo.
//
// No es recursivo: el layout es `<dir>/<filename>` plano (blob,
// .info, posibles intermedios). Suficiente.
func (g *GC) isStale(dir string, cutoff time.Time) (bool, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	if len(ents) == 0 {
		// Dir vacío — borrable.
		return true, nil
	}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			return false, err
		}
		if info.ModTime().After(cutoff) {
			return false, nil
		}
	}
	return true, nil
}
