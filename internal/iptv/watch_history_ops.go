package iptv

// WatchHistoryOps gestiona el historial de visualización. Recibe
// beacons del player y alimenta la rail "Continue Watching".
// La traducción channel-id ↔ stream-url (clave DB que sobrevive
// re-imports M3U) es interna a este sub-service.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
)

// WatchHistoryOps necesita channels repo para mapear channelID →
// stream_url antes de persistir.
type WatchHistoryOps struct {
	channels     *db.ChannelRepository
	watchHistory *db.ChannelWatchHistoryRepository
}

func newWatchHistoryOps(channels *db.ChannelRepository, watchHistory *db.ChannelWatchHistoryRepository) *WatchHistoryOps {
	return &WatchHistoryOps{
		channels:     channels,
		watchHistory: watchHistory,
	}
}

// RecordWatch upserts un par (user, channel) en la watch history.
// Resuelve primero la stream_url del canal para que la fila sobreviva
// al siguiente refresh M3U (UUIDs cambian, URLs no).
//
// Devuelve el timestamp escrito así el HTTP handler puede echoarlo
// al cliente sin un segundo read.
//
// Devuelve ErrChannelNotFound si el canal fue dropeado de la library
// desde que el beacon se disparó. El caller debe traducirlo a 404 —
// una race entre channel removal y beacon pendiente NO debería
// aparecer como server error.
func (w *WatchHistoryOps) RecordWatch(ctx context.Context, userID, channelID string) (time.Time, error) {
	if w.watchHistory == nil {
		return time.Time{}, fmt.Errorf("watch history not configured")
	}
	ch, err := w.channels.GetByID(ctx, channelID)
	if err != nil {
		if errors.Is(err, db.ErrChannelNotFound) {
			return time.Time{}, err
		}
		return time.Time{}, fmt.Errorf("get channel: %w", err)
	}
	return w.watchHistory.RecordByStreamURL(ctx, userID, ch.StreamURL)
}

// ListContinueWatching devuelve los canales más recientemente vistos
// del user, newest first, capado a limit. Filtrado de library ACL NO
// se aplica aquí — el caller debe pasar el filter basado en el
// contexto del request. Callers del surface admin (donde el admin ve
// todas las libraries) pueden skipear el filter por completo.
//
// accessibleLibraries nil = sin filtering; empty map = deny
// everything; un map populated mantiene sólo canales cuyo LibraryID
// es una key.
func (w *WatchHistoryOps) ListContinueWatching(
	ctx context.Context,
	userID string,
	limit int,
	accessibleLibraries map[string]bool,
) ([]*iptvmodel.Channel, []time.Time, error) {
	if w.watchHistory == nil {
		return nil, nil, nil
	}
	// Fetch extras así el post-filter (access) sigue devolviendo
	// hasta `limit` — denials patológicos truncarían el rail bajo
	// su tamaño intencional sin esto.
	fetch := limit
	if accessibleLibraries != nil {
		fetch = limit * 2
		if fetch < 10 {
			fetch = 10
		}
	}
	channels, watched, err := w.watchHistory.ListChannelsByUser(ctx, userID, fetch)
	if err != nil {
		return nil, nil, err
	}
	if accessibleLibraries == nil {
		if len(channels) > limit {
			channels = channels[:limit]
			watched = watched[:limit]
		}
		return channels, watched, nil
	}
	outCh := make([]*iptvmodel.Channel, 0, len(channels))
	outTs := make([]time.Time, 0, len(channels))
	for i, ch := range channels {
		if accessibleLibraries[ch.LibraryID] {
			outCh = append(outCh, ch)
			outTs = append(outTs, watched[i])
			if len(outCh) >= limit {
				break
			}
		}
	}
	return outCh, outTs, nil
}
