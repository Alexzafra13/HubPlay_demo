package federation

// Metodos de Manager para estado de reproduccion cross-peer.

import (
	"context"
	"fmt"
)

// ProgressUpdate agrupa parametros de RecordProgress. DurationTicks
// puede ser 0 (el upsert preserva valor previo no-zero).
// Completed=true quita la fila del rail Continue Watching.
type ProgressUpdate struct {
	UserID        string
	PeerID        string
	RemoteItemID  string
	PositionTicks int64
	DurationTicks int64
	Completed     bool
}

// RecordProgress escribe la posicion de reproduccion para un item
// federado. Valida que el peer este paired para evitar ghost rows.
func (m *Manager) RecordProgress(ctx context.Context, update ProgressUpdate) error {
	if update.UserID == "" || update.PeerID == "" || update.RemoteItemID == "" {
		return fmt.Errorf("federation: record progress: missing identifier")
	}
	peer, err := m.repo.GetPeerByID(ctx, update.PeerID)
	if err != nil {
		return fmt.Errorf("federation: record progress: lookup peer: %w", err)
	}
	if !peer.IsActive() {
		// Descartar silenciosamente: UI puede tener sesion stale
		// mientras admin revoca; no queremos persistir ni mostrar error.
		return nil
	}
	now := m.clock.Now()
	return m.repo.UpsertProgress(ctx, &Progress{
		UserID:        update.UserID,
		PeerID:        update.PeerID,
		RemoteItemID:  update.RemoteItemID,
		PositionTicks: update.PositionTicks,
		DurationTicks: update.DurationTicks,
		Completed:     update.Completed,
		LastPlayedAt:  now,
		UpdatedAt:     now,
	})
}

// GetProgress devuelve el estado de reproduccion o nil (= empezar de 0).
func (m *Manager) GetProgress(ctx context.Context, userID, peerID, remoteItemID string) (*Progress, error) {
	if userID == "" || peerID == "" || remoteItemID == "" {
		return nil, fmt.Errorf("federation: get progress: missing identifier")
	}
	return m.repo.GetProgress(ctx, userID, peerID, remoteItemID)
}

// ListContinueWatching es el rail cross-peer Continue Watching.
// El repo filtra (no completado, >0, <90%, peer paired).
func (m *Manager) ListContinueWatching(ctx context.Context, userID string, limit int) ([]*PeerContinueWatchingItem, error) {
	if userID == "" {
		return nil, fmt.Errorf("federation: list continue watching: missing user id")
	}
	return m.repo.ListContinueWatching(ctx, userID, limit)
}
