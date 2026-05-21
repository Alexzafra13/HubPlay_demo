package iptv

// HealthOps aísla el surface de channel health (probe outcomes +
// health summary + reset) del olor CC del audit 2026-05-14. Lleva su
// propio estado de "último bucket publicado por canal" (healthMu +
// lastKnownBucket map) que NO comparte con nadie en Service — por
// eso es buen candidato a sub-service independiente.
//
// Implementa la interfaz `iptv.ChannelHealthReporter` que el stream
// proxy usa para persistir probe outcomes sin importar `db`. El
// embedding en Service preserva esa interfaz vía method promotion.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
	iptvmodel "hubplay/internal/iptv/model"
)

// channelHealthProbeTimeout capa cuánto tiene el repo para grabar
// una probe outcome. Bound tight para que un slow SQLite WAL flush
// no pinee la goroutine del proxy durante varios segundos de
// latency noise.
const channelHealthProbeTimeout = 2 * time.Second

// HealthOps lleva sus propios mutex + map para el gating del
// ChannelHealthChanged event (sólo publicar en transiciones reales,
// no en cada probe tick). El `pub` es compartido por puntero con
// Service así un único SetEventBus actualiza ambos a la vez.
type HealthOps struct {
	channels *db.ChannelRepository
	logger   *slog.Logger
	pub      *publisher

	healthMu sync.Mutex
	// lastKnownBucket mapea channelID → último health bucket publicado
	// ("ok" / "degraded" / "dead"). Gate de eventos: emitimos sólo
	// en transiciones reales. In-memory only — en restart re-emitimos
	// en el primer probe per channel, que es lo que el admin quiere.
	lastKnownBucket map[string]string
}

func newHealthOps(channels *db.ChannelRepository, pub *publisher, logger *slog.Logger) *HealthOps {
	return &HealthOps{
		channels:        channels,
		logger:          logger,
		pub:             pub,
		lastKnownBucket: make(map[string]string),
	}
}

// healthBucket mapea un raw consecutive-failure count al wire bucket
// que el admin UI consume. Mirror de handlers.deriveHealthStatus —
// duplicado aquí para evitar import cycle (handlers → iptv).
// Mantener ambos en sync; UnhealthyThreshold es el único tunable.
func healthBucket(consecutiveFailures int) string {
	switch {
	case consecutiveFailures <= 0:
		return "ok"
	case consecutiveFailures >= db.UnhealthyThreshold:
		return "dead"
	default:
		return "degraded"
	}
}

// RecordProbeSuccess satisface iptv.ChannelHealthReporter. Logs en
// DB error y sigue — al stream proxy no le importa si realmente
// persistimos el state.
func (h *HealthOps) RecordProbeSuccess(ctx context.Context, channelID string) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := h.channels.RecordProbeSuccess(ctx, channelID); err != nil {
		h.logger.Debug("record probe success", "channel", channelID, "error", err)
		return
	}
	h.maybePublishHealthChange(ctx, channelID)
}

// RecordProbeFailure satisface iptv.ChannelHealthReporter. Traduce
// el Go error a un string corto y user-safe antes de persistir.
func (h *HealthOps) RecordProbeFailure(ctx context.Context, channelID string, probeErr error) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := h.channels.RecordProbeFailure(ctx, channelID, sanitiseProbeError(probeErr)); err != nil {
		h.logger.Debug("record probe failure", "channel", channelID, "error", err)
		return
	}
	h.maybePublishHealthChange(ctx, channelID)
}

// maybePublishHealthChange compara el bucket post-write del canal
// con el último que publicamos para él; si cambió, emite un evento
// ChannelHealthChanged para que los suscriptores SSE (admin
// dashboard) invaliden caches sin polling. No-op cleanly cuando el
// event bus no está cableado (tests, partial bring-up).
//
// Por qué post-write read en lugar de in-memory math: probe y
// playback failures ambos incrementan el counter por code paths
// distintos (proxy probe, prober worker, beacon /playback-failure).
// Leer state canónico evita race sobre un count derived.
func (h *HealthOps) maybePublishHealthChange(ctx context.Context, channelID string) {
	if h.pub == nil || h.pub.bus == nil {
		return
	}
	ch, err := h.channels.GetByID(ctx, channelID)
	if err != nil || ch == nil {
		return
	}
	bucket := healthBucket(ch.ConsecutiveFailures)

	h.healthMu.Lock()
	prev, seen := h.lastKnownBucket[channelID]
	h.lastKnownBucket[channelID] = bucket
	h.healthMu.Unlock()

	// Bootstrap silence: la primera observación de un canal después
	// del proceso start seedea el map pero nunca publica. Sin esto,
	// el primer probe pass tras un restart dispara N eventos (uno
	// por canal) y el admin UI ve una stampede de invalidación en
	// la query unhealthy-list — puro ruido, ya que el fetch inicial
	// del UI al mount ya refleja la verdad. Transiciones steady-state
	// siguen publicando normalmente.
	if !seen {
		return
	}
	if prev == bucket {
		return
	}
	h.pub.publish(event.Event{
		Type: event.ChannelHealthChanged,
		Data: map[string]any{
			"channel_id":           ch.ID,
			"library_id":           ch.LibraryID,
			"health_status":        bucket,
			"consecutive_failures": ch.ConsecutiveFailures,
		},
	})
}

// sanitiseProbeError trimea prefijos de wrapping repetidos y stripea
// noise obviamente-transient para que el admin UI muestre la cause
// underlying ("no such host", "connection refused", "HTTP 403") en
// lugar de un nest de prefijos wrapper.
func sanitiseProbeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, prefix := range []string{"connect: ", "fetch: ", "read upstream: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// ListUnhealthyChannels devuelve canales cuyo consecutive-failure
// count está at-or-above el threshold. Threshold 0 usa el default
// del repo (db.UnhealthyThreshold).
func (h *HealthOps) ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*iptvmodel.Channel, error) {
	return h.channels.ListUnhealthyByLibrary(ctx, libraryID, threshold)
}

// ChannelHealthSummary es lo que el panel admin Bibliotecas lee on
// page load para renderear el status dot, la strip de stats y los
// counts de tab badge — sin arrastrar cada channel row por el wire
// sólo para llamar a .length sobre el result. Las listas full
// cargan lazy, sólo cuando el operator clickea en la tab.
//
// Window matchea ChannelWithoutEPGWindow del read path así el count
// "sin EPG" cuadra con lo que ListChannelsWithoutEPG devolvería si
// se llamase para la misma library.
func (h *HealthOps) ChannelHealthSummary(ctx context.Context, libraryID string) (iptvmodel.ChannelHealthSummary, error) {
	now := time.Now().UTC()
	return h.channels.HealthSummaryByLibrary(ctx, libraryID,
		now.Add(-2*time.Hour), now.Add(ChannelWithoutEPGWindow))
}

// ResetChannelHealth limpia el health state para un canal así
// reaparece en la lista user-facing en el next render. Usado por
// la acción admin "marcar como OK".
func (h *HealthOps) ResetChannelHealth(ctx context.Context, channelID string) error {
	if err := h.channels.ResetHealth(ctx, channelID); err != nil {
		return err
	}
	// Ctx fresh background así el publish no está atado al deadline
	// del caller (el reset ya tuvo success).
	pubCtx, cancel := context.WithTimeout(context.Background(), channelHealthProbeTimeout)
	defer cancel()
	h.maybePublishHealthChange(pubCtx, channelID)
	return nil
}
