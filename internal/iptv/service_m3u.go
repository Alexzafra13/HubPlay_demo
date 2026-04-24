package iptv

// M3U import — downloads the playlist, replaces the library's channels
// and re-applies any admin-edited overrides. If the playlist advertises
// a `url-tvg` we opportunistically kick off an EPG refresh so the guide
// populates on the same import cycle.

import (
	"context"
	"fmt"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// RefreshM3U downloads and parses an M3U playlist, replacing channels for the library.
func (s *Service) RefreshM3U(ctx context.Context, libraryID string) (int, error) {
	s.mu.Lock()
	if s.refreshes[libraryID] {
		s.mu.Unlock()
		return 0, fmt.Errorf("refresh already in progress for library %s", libraryID)
	}
	s.refreshes[libraryID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.refreshes, libraryID)
		s.mu.Unlock()
	}()

	lib, err := s.libraries.GetByID(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("get library: %w", err)
	}

	if lib.M3UURL == "" {
		return 0, fmt.Errorf("library %s has no M3U URL configured", libraryID)
	}

	s.logger.Info("refreshing M3U playlist", "library", libraryID, "url", lib.M3UURL)

	body, err := s.fetchURL(ctx, lib.M3UURL)
	if err != nil {
		return 0, fmt.Errorf("fetch M3U: %w", err)
	}
	defer body.Close() //nolint:errcheck

	playlist, err := ParseM3U(body)
	if err != nil {
		return 0, fmt.Errorf("parse M3U: %w", err)
	}

	now := time.Now()
	dbChannels := make([]*db.Channel, 0, len(playlist.Channels))
	for i, ch := range playlist.Channels {
		dbChannels = append(dbChannels, &db.Channel{
			ID:        generateID(),
			LibraryID: libraryID,
			Name:      ch.Name,
			Number:    assignNumber(ch.Number, i+1),
			GroupName: ch.GroupName,
			LogoURL:   ch.LogoURL,
			StreamURL: ch.StreamURL,
			TvgID:     ch.TvgID,
			Language:  ch.Language,
			Country:   ch.Country,
			IsActive:  true,
			AddedAt:   now,
		})
	}

	if err := s.channels.ReplaceForLibrary(ctx, libraryID, dbChannels); err != nil {
		return 0, fmt.Errorf("replace channels: %w", err)
	}

	// Re-apply hand-edited channel fields (currently tvg_id) that the
	// admin configured via PATCH /channels/{id}. The overrides table is
	// keyed by stream URL so the fresh channel rows inherited from the
	// M3U get their operator-intent restored. Orphaned overrides (URL
	// dropped from the playlist) are a no-op and stay in the table in
	// case the URL returns later.
	if s.overrides != nil {
		if n, err := s.overrides.ApplyToLibrary(ctx, libraryID); err != nil {
			// A failure here shouldn't roll back the M3U refresh —
			// channels are saved, overrides just didn't reapply. Logged
			// loudly so the admin notices.
			s.logger.Error("apply channel overrides post-import",
				"library", libraryID, "error", err)
		} else if n > 0 {
			s.logger.Info("reapplied channel overrides",
				"library", libraryID, "count", n)
		}
	}

	// Persist any XMLTV URL the playlist advertised so the EPG refresher has
	// something to fetch. We only overwrite when the library has no URL
	// configured — an operator-set URL wins over whatever the feed suggests.
	epgDiscovered := false
	if playlist.EPGURL != "" && lib.EPGURL == "" {
		lib.EPGURL = playlist.EPGURL
		lib.UpdatedAt = now
		if err := s.libraries.Update(ctx, lib); err != nil {
			// Don't fail the whole refresh — the channels are already saved,
			// and the EPG URL is nice-to-have. Log and move on.
			s.logger.Warn("persist discovered EPG URL",
				"library", libraryID, "epg_url", playlist.EPGURL, "error", err)
		} else {
			epgDiscovered = true
			s.logger.Info("discovered EPG URL from playlist header",
				"library", libraryID, "epg_url", playlist.EPGURL)
		}
	}

	s.logger.Info("M3U refresh complete", "library", libraryID, "channels", len(dbChannels))
	s.publish(event.Event{
		Type: event.PlaylistRefreshed,
		Data: map[string]any{
			"library_id":     libraryID,
			"channels_count": len(dbChannels),
		},
	})

	// Kick off an EPG refresh for newly-discovered URLs so the guide
	// populates on the same import cycle. Fire-and-forget with a detached
	// context: the import response should not block on a potentially-slow
	// XMLTV download. Errors are logged inside RefreshEPG.
	if epgDiscovered {
		go func(id string) {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if _, err := s.RefreshEPG(bg, id); err != nil {
				s.logger.Warn("auto-trigger EPG refresh after M3U import",
					"library", id, "error", err)
			}
		}(libraryID)
	}

	return len(dbChannels), nil
}
