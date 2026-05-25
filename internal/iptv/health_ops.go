package iptv

// HealthOps gestiona channel health (probe outcomes + summary + reset).
// Lleva su propio estado (healthMu + lastKnownBucket) que no comparte
// con Service. Implementa ChannelHealthReporter vía method promotion.

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

// channelHealthProbeTimeout — límite para grabar una probe outcome.
// Tight para que un flush lento de SQLite WAL no bloquee el proxy.
const channelHealthProbeTimeout = 2 * time.Second

// HealthOps — mutex + map propios para emitir ChannelHealthChanged
// solo en transiciones reales. pub compartido con Service.
type HealthOps struct {
	channels *db.ChannelRepository
	logger   *slog.Logger
	pub      *publisher

	healthMu sync.Mutex
	// lastKnownBucket — gate de eventos: solo emitimos en transiciones.
	// In-memory: en restart re-emitimos en el primer probe per canal.
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

// healthBucket mapea consecutive-failures al bucket del admin UI.
// Duplicado de handlers.deriveHealthStatus para evitar import cycle.
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

// RecordProbeSuccess — satisface ChannelHealthReporter. Errores de
// DB se logean y se ignoran (el proxy no depende de la persistencia).
func (h *HealthOps) RecordProbeSuccess(ctx context.Context, channelID string) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := h.channels.RecordProbeSuccess(ctx, channelID); err != nil {
		h.logger.Debug("record probe success", "channel", channelID, "error", err)
		return
	}
	h.maybePublishHealthChange(ctx, channelID)
}

// RecordProbeFailure — traduce el error a string corto antes de persistir.
func (h *HealthOps) RecordProbeFailure(ctx context.Context, channelID string, probeErr error) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := h.channels.RecordProbeFailure(ctx, channelID, sanitiseProbeError(probeErr)); err != nil {
		h.logger.Debug("record probe failure", "channel", channelID, "error", err)
		return
	}
	h.maybePublishHealthChange(ctx, channelID)
}

// maybePublishHealthChange emite ChannelHealthChanged solo si el
// bucket cambió. Post-write read (no math in-memory) porque probe
// y playback failures incrementan por paths distintos.
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

	// Bootstrap silence: la primera observación seedea el map sin
	// publicar. Sin esto, el primer probe post-restart dispara N
	// eventos que son puro ruido (el fetch inicial del UI ya refleja
	// la verdad).
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

// sanitiseProbeError limpia prefijos wrapper para que el admin UI
// muestre la causa subyacente ("no such host", "connection refused").
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

// ListUnhealthyChannels devuelve canales con failures >= threshold.
// Threshold 0 usa db.UnhealthyThreshold por defecto.
func (h *HealthOps) ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*iptvmodel.Channel, error) {
	return h.channels.ListUnhealthyByLibrary(ctx, libraryID, threshold)
}

// ChannelHealthSummary — rollup ligero para el status dot y badges
// del panel admin, sin arrastrar cada channel row.
func (h *HealthOps) ChannelHealthSummary(ctx context.Context, libraryID string) (iptvmodel.ChannelHealthSummary, error) {
	now := time.Now().UTC()
	return h.channels.HealthSummaryByLibrary(ctx, libraryID,
		now.Add(-2*time.Hour), now.Add(ChannelWithoutEPGWindow))
}

// ResetChannelHealth limpia el health state de un canal (acción admin).
func (h *HealthOps) ResetChannelHealth(ctx context.Context, channelID string) error {
	if err := h.channels.ResetHealth(ctx, channelID); err != nil {
		return err
	}
	// Ctx background para que el publish no dependa del deadline del caller.
	pubCtx, cancel := context.WithTimeout(context.Background(), channelHealthProbeTimeout)
	defer cancel()
	h.maybePublishHealthChange(pubCtx, channelID)
	return nil
}
