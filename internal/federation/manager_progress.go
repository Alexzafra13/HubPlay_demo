package federation

// Manager methods for cross-peer playback state — the user pressed
// play on a federated item, the position is persisted under
// (user, peer, remote_item) so Continue Watching surfaces it back.
// Lifted out of manager.go so the progress lifecycle is one
// self-contained slice.

import (
	"context"
	"fmt"
)

// ProgressUpdate agrupa los parámetros de `RecordProgress`. Cierra el
// olor F14-2-b del audit 2026-05-14 (función de 7 params posicionales,
// 3 de ellos `string` consecutivos y 2 `int64` consecutivos — fácil
// de confundir orden en review).
//
// `DurationTicks` puede ser 0 en la primera llamada; el upsert
// preserva un valor previo no-zero, así que una vez que el player
// aprende la duración del manifest, queda pinned para la vida de la
// fila.
//
// `Completed=true` borra la fila del rail Continue Watching (mismo
// gate que `user_data.completed=1` local).
type ProgressUpdate struct {
	UserID        string
	PeerID        string
	RemoteItemID  string
	PositionTicks int64
	DurationTicks int64
	Completed     bool
}

// RecordProgress writes the user's playback position for a federated
// item. Validates that (PeerID, RemoteItemID) names a peer que está
// actualmente paired -- de lo contrario aceptaríamos progress para
// items que el user no puede alcanzar, lo cual aparecería como ghost
// rows en Continue Watching.
func (m *Manager) RecordProgress(ctx context.Context, update ProgressUpdate) error {
	if update.UserID == "" || update.PeerID == "" || update.RemoteItemID == "" {
		return fmt.Errorf("federation: record progress: missing identifier")
	}
	peer, err := m.repo.GetPeerByID(ctx, update.PeerID)
	if err != nil {
		return fmt.Errorf("federation: record progress: lookup peer: %w", err)
	}
	if !peer.IsActive() {
		// Drop progress writes against revoked / pending peers
		// silently -- the UI may still hold a stale player session
		// while the admin revokes; we don't want to surface that as
		// an error to the user, and we don't want to persist the
		// row either.
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

// GetProgress returns the user's playback state for a federated item,
// or nil when there is no recorded position. Callers (PeerItemDetail
// resume button) treat nil as "start from 0".
func (m *Manager) GetProgress(ctx context.Context, userID, peerID, remoteItemID string) (*Progress, error) {
	if userID == "" || peerID == "" || remoteItemID == "" {
		return nil, fmt.Errorf("federation: get progress: missing identifier")
	}
	return m.repo.GetProgress(ctx, userID, peerID, remoteItemID)
}

// ListContinueWatching is the cross-peer Continue Watching rail. The
// repo handles the in-progress filtering (completed=0, position>0,
// <90% played, peer still paired); we just delegate. The order
// matches local ContinueWatching: most-recently-played first.
func (m *Manager) ListContinueWatching(ctx context.Context, userID string, limit int) ([]*PeerContinueWatchingItem, error) {
	if userID == "" {
		return nil, fmt.Errorf("federation: list continue watching: missing user id")
	}
	return m.repo.ListContinueWatching(ctx, userID, limit)
}
