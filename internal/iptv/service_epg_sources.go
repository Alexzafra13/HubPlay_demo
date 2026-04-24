package iptv

// EPG source management (admin) — CRUD for the multi-provider EPG
// model. Admin-facing only; the handler layer gates these behind the
// admin role. The service itself validates shape and catalog integrity.

import (
	"context"
	"fmt"

	"hubplay/internal/db"
)

// ListEPGSources returns the EPG providers configured for a library,
// ordered by priority ascending (the order the refresher processes
// them in). Empty slice if the library has none.
func (s *Service) ListEPGSources(ctx context.Context, libraryID string) ([]*db.LibraryEPGSource, error) {
	return s.epgSources.ListByLibrary(ctx, libraryID)
}

// AddEPGSource attaches a new provider to a library. Either catalogID
// or url must be non-empty; when both are set the catalog entry's URL
// wins and the caller's `url` is ignored (prevents drift where the
// admin pastes a stale URL for a known catalog entry).
func (s *Service) AddEPGSource(ctx context.Context, libraryID, catalogID, customURL string) (*db.LibraryEPGSource, error) {
	if _, err := s.libraries.GetByID(ctx, libraryID); err != nil {
		return nil, fmt.Errorf("get library: %w", err)
	}

	src := &db.LibraryEPGSource{
		ID:        generateID(),
		LibraryID: libraryID,
	}
	if catalogID != "" {
		entry, ok := FindEPGSource(catalogID)
		if !ok {
			return nil, fmt.Errorf("unknown catalog EPG source %q", catalogID)
		}
		src.CatalogID = catalogID
		src.URL = entry.URL
	} else {
		if customURL == "" {
			return nil, fmt.Errorf("either catalog_id or url is required")
		}
		src.URL = customURL
	}

	if err := s.epgSources.Create(ctx, src); err != nil {
		return nil, err
	}
	return src, nil
}

// RemoveEPGSource deletes one provider by id. Does not purge any EPG
// programmes the source contributed — that happens on the next
// RefreshEPG when the merge runs without it.
func (s *Service) RemoveEPGSource(ctx context.Context, libraryID, sourceID string) error {
	src, err := s.epgSources.GetByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("get source: %w", err)
	}
	if src == nil || src.LibraryID != libraryID {
		return fmt.Errorf("source %s not found in library %s", sourceID, libraryID)
	}
	return s.epgSources.Delete(ctx, sourceID)
}

// ReorderEPGSources rewrites every source's priority to match the
// order the caller provides. The list must contain exactly the ids
// currently attached to the library — no adds, no removes. Anything
// else is rejected to avoid partial writes.
func (s *Service) ReorderEPGSources(ctx context.Context, libraryID string, orderedIDs []string) error {
	current, err := s.epgSources.ListByLibrary(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	if len(orderedIDs) != len(current) {
		return fmt.Errorf("reorder list has %d ids, library has %d sources", len(orderedIDs), len(current))
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.ID] = true
	}
	for _, id := range orderedIDs {
		if !seen[id] {
			return fmt.Errorf("source %s is not attached to library %s", id, libraryID)
		}
	}
	return s.epgSources.UpdatePriorities(ctx, libraryID, orderedIDs)
}

// PublicEPGCatalog exposes the curated catalog to the API layer so
// the admin UI can render a dropdown without duplicating the list.
func (s *Service) PublicEPGCatalog() []PublicEPGSource {
	return PublicEPGSources()
}
