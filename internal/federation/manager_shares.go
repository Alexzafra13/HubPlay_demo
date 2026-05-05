package federation

// Manager methods for the per-peer library share table — opting in
// (or out) of which local libraries each paired peer can browse, play,
// download, or stream live TV from. Lifted out of manager.go so the
// share lifecycle reads as one self-contained slice.

import (
	"context"

	"github.com/google/uuid"

	"hubplay/internal/domain"
)

// ShareLibrary opts a local library into being visible to the named
// peer with the given scopes. Idempotent — re-calling with different
// scopes updates the existing row (UPSERT); the admin can liberalise
// or tighten without manually unsharing first.
//
// Validates the peer is paired before persisting; a revoked or
// pending peer can't have shares because the row would be unreachable
// anyway.
func (m *Manager) ShareLibrary(ctx context.Context, peerID, libraryID, createdByUserID string, scopes ShareScopes) (*LibraryShare, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, domain.ErrPeerNotFound
	}
	if peer.Status != PeerPaired {
		return nil, domain.ErrPeerUnauthorized
	}
	share := &LibraryShare{
		ID:              uuid.NewString(),
		PeerID:          peerID,
		LibraryID:       libraryID,
		CanBrowse:       scopes.CanBrowse,
		CanPlay:         scopes.CanPlay,
		CanDownload:     scopes.CanDownload,
		CanLiveTV:       scopes.CanLiveTV,
		CreatedByUserID: createdByUserID,
		CreatedAt:       m.clock.Now(),
	}
	if err := m.repo.UpsertLibraryShare(ctx, share); err != nil {
		return nil, err
	}
	m.publish(EventShareAdded, map[string]any{
		"peer_id":    peerID,
		"library_id": libraryID,
		"share_id":   share.ID,
		"scopes":     scopes,
	})
	// Re-read so the returned row matches what the DB persisted (in
	// case the unique conflict path overwrote an existing share).
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// UnshareLibrary removes a single share row by ID. Idempotent — a
// missing share is treated as success because the desired state
// (peer cannot see this library) is already true.
func (m *Manager) UnshareLibrary(ctx context.Context, peerID, shareID string) error {
	if err := m.repo.DeleteLibraryShare(ctx, peerID, shareID); err != nil {
		return err
	}
	m.publish(EventShareRemoved, map[string]any{
		"peer_id":  peerID,
		"share_id": shareID,
	})
	return nil
}

// ListSharesByPeer returns every share row for the given peer. Powers
// the admin UI's per-peer expansion panel.
func (m *Manager) ListSharesByPeer(ctx context.Context, peerID string) ([]*LibraryShare, error) {
	return m.repo.ListSharesByPeer(ctx, peerID)
}

// GetLibraryShare returns the share row for one (peer, library) pair,
// or nil when the peer has no share for the library. Used by the
// federation streaming handler to gate "is this item playable by the
// requesting peer?" with one round-trip: look up the item's library,
// look up the share, check CanPlay.
func (m *Manager) GetLibraryShare(ctx context.Context, peerID, libraryID string) (*LibraryShare, error) {
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// ListSharedLibrariesForPeer returns the libraries the peer can see —
// the data shape served by GET /peer/libraries. Server-side filter
// via JOIN; the peer cannot reach libraries without rows.
func (m *Manager) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	return m.repo.ListSharedLibrariesForPeer(ctx, peerID)
}

// ListSharedItems returns items in a shared library, paginated.
// Returns ErrPeerNotFound if the peer has no share for this library
// — we deliberately conflate "library doesn't exist" and "library
// not shared with you" so attackers can't enumerate library IDs.
func (m *Manager) ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error) {
	share, err := m.repo.GetLibraryShare(ctx, peerID, libraryID)
	if err != nil {
		return nil, 0, err
	}
	if share == nil || !share.CanBrowse {
		return nil, 0, domain.ErrPeerNotFound
	}
	return m.repo.ListSharedItems(ctx, peerID, libraryID, offset, limit)
}
