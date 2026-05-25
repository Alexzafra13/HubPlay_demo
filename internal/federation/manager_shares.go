package federation

// Metodos de Manager para la tabla de shares de bibliotecas por peer.

import (
	"context"

	"github.com/google/uuid"

	"hubplay/internal/domain"
)

// ShareLibrary habilita una biblioteca local para un peer con los scopes
// dados. Idempotente (UPSERT). Valida que el peer este paired.
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
	// Re-leer para devolver lo que la DB persisto (posible UPSERT).
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// UnshareLibrary elimina un share por ID. Idempotente.
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

// ListSharesByPeer devuelve todos los shares de un peer.
func (m *Manager) ListSharesByPeer(ctx context.Context, peerID string) ([]*LibraryShare, error) {
	return m.repo.ListSharesByPeer(ctx, peerID)
}

// GetLibraryShare devuelve el share para (peer, library) o nil.
func (m *Manager) GetLibraryShare(ctx context.Context, peerID, libraryID string) (*LibraryShare, error) {
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// ListSharedLibrariesForPeer devuelve las bibliotecas visibles para el peer.
func (m *Manager) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	return m.repo.ListSharedLibrariesForPeer(ctx, peerID)
}

// ListSharedItems devuelve items paginados de una biblioteca compartida.
// Devuelve ErrPeerNotFound si no hay share (confunde "no existe" con
// "no compartida" para evitar enumeracion).
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
