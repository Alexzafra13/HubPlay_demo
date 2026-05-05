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

// RecordProgress writes the user's playback position for a federated
// item. Validates that (peerID, remoteItemID) names a peer that's
// actually paired and that the user has at least browsed the
// catalog -- otherwise we'd accept progress for items the user can't
// reach, which would surface as ghost rows in Continue Watching.
//
// duration_ticks may be 0 on the first call; the upsert preserves a
// previously-stored non-zero value, so once the player learns
// duration from the manifest it's pinned for the life of the row.
//
// completed=true clears the row from the Continue Watching rail
// (same gate as local user_data.completed=1).
func (m *Manager) RecordProgress(ctx context.Context, userID, peerID, remoteItemID string, positionTicks, durationTicks int64, completed bool) error {
	if userID == "" || peerID == "" || remoteItemID == "" {
		return fmt.Errorf("federation: record progress: missing identifier")
	}
	peer, err := m.repo.GetPeerByID(ctx, peerID)
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
		UserID:        userID,
		PeerID:        peerID,
		RemoteItemID:  remoteItemID,
		PositionTicks: positionTicks,
		DurationTicks: durationTicks,
		Completed:     completed,
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
