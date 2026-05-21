package main

import (
	"context"
	"log/slog"
)

// stopFn es la firma común de las funciones de teardown registradas en
// lifecycle. Devolver error es opcional — los stops históricamente no
// devuelven nada y se wrapean a `return nil` al registrar, pero el tipo
// soporta los que sí (HTTP server.Shutdown, iptvProber.Stop).
type stopFn = func(ctx context.Context) error

// stopHook empareja un nombre legible (para logs) con su función de
// teardown. No hay otros campos — el nombre se usa sólo en
// "shutdown step completed/failed" logs.
type stopHook struct {
	name string
	fn   stopFn
}

// lifecycle agrupa los componentes long-lived registrados durante el
// boot del binario y dirige el shutdown en TRES FASES explícitas
// (ordenación por dominio, no por LIFO de init order):
//
//  1. **Workers** — background jobs independientes del HTTP server
//     (iptv scheduler, iptv prober worker, scan scheduler, image
//     refresh scheduler, auth session cleaner, retention runner).
//     Se paran PRIMERO en add-order para que dejen de generar nueva
//     actividad antes de empezar a tirar abajo el resto. Esto
//     evita que un refresh recién arrancado choque contra una DB
//     que está a punto de cerrarse.
//
//  2. **HTTP server** — drain. `server.Shutdown(ctx)` espera a que
//     los requests in-flight terminen (bounded por shutdownCtx). Se
//     ejecuta entre workers y services porque los workers ya están
//     parados (no van a chocar) y los services todavía hacen falta
//     para que los requests in-flight respondan.
//
//  3. **Services** — componentes HTTP-coupled (stream manager,
//     iptv proxy / transmux / service, library service). Se paran
//     en **LIFO** (reverse-of-add) — los wirings más tardíos son los
//     que más probablemente dependen de los anteriores, así que
//     tirarlos primero respeta el grafo de dependencias.
//
// Cierra parcialmente el olor G del audit 2026-05-14: el `runtime`
// god-struct (16 campos posicionales que `waitForShutdown` desempaquetaba
// uno a uno) desaparece. El comentario que admitía el síntoma como
// solución ("adding a new bg service is now a one-line struct-field
// append") se sustituye por una sola llamada `lc.AddWorker/AddService`
// junto al wiring. Feature modules per-paquete (library.Module,
// iptv.Module) que cerrarían el olor al 100% quedan deferred como
// sesiones propias.
type lifecycle struct {
	workers  []stopHook
	services []stopHook
}

// AddWorker registra un background job (goroutine independiente de
// HTTP). Se ejecuta en fase 1 del shutdown, en add-order.
func (lc *lifecycle) AddWorker(name string, fn stopFn) {
	lc.workers = append(lc.workers, stopHook{name: name, fn: fn})
}

// AddService registra un componente HTTP-coupled. Se ejecuta en fase 3,
// en LIFO (reverse-of-add).
func (lc *lifecycle) AddService(name string, fn stopFn) {
	lc.services = append(lc.services, stopHook{name: name, fn: fn})
}

// stopWorkers ejecuta todas las paradas de fase 1 en add-order.
// Errores se loggean pero NO abortan la cadena — cada hook tiene
// derecho a su chance de cleanup aunque uno anterior falle.
func (lc *lifecycle) stopWorkers(ctx context.Context, logger *slog.Logger) {
	for _, h := range lc.workers {
		runHook(ctx, h, logger)
	}
}

// stopServices ejecuta las paradas de fase 3 en LIFO.
func (lc *lifecycle) stopServices(ctx context.Context, logger *slog.Logger) {
	for i := len(lc.services) - 1; i >= 0; i-- {
		runHook(ctx, lc.services[i], logger)
	}
}

func runHook(ctx context.Context, h stopHook, logger *slog.Logger) {
	if err := h.fn(ctx); err != nil {
		logger.Warn("shutdown hook failed", "name", h.name, "error", err)
		return
	}
	logger.Info("shutdown hook completed", "name", h.name)
}
