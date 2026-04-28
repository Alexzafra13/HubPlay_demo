package iptv

// M3U import — downloads the playlist, replaces the library's channels
// and re-applies any admin-edited overrides. If the playlist advertises
// a `url-tvg` we opportunistically kick off an EPG refresh so the guide
// populates on the same import cycle.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// RefreshM3U downloads and parses an M3U playlist, replacing channels for the library.
func (s *Service) RefreshM3U(ctx context.Context, libraryID string) (int, error) {
	s.mu.Lock()
	if s.refreshes[libraryID] {
		s.mu.Unlock()
		return 0, fmt.Errorf("library %s: %w", libraryID, ErrRefreshInProgress)
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

	// Streaming parse: emit each entry through a callback so we can
	// filter VOD + language on the fly and never hold the whole
	// playlist in memory. Xtream-Codes "M3U_PLUS" exports bundle live
	// + movies + series and routinely run into hundreds of thousands
	// of entries.
	now := time.Now()
	var (
		dbChannels      []*db.Channel
		index           int
		vodSkipped      int
		languageSkipped int
	)
	allowedLangs := parseLanguageFilter(lib.LanguageFilter)
	playlistEPGURL, parsedLines, parseErr := ParseM3UStream(body, func(ch M3UChannel) error {
		if IsVODChannel(ch) {
			vodSkipped++
			return nil
		}
		// Language filter — applied AFTER VOD so the operator-visible
		// "vod_skipped" stat reflects the same denominator regardless
		// of language config. Empty allowlist = no filter.
		if !MatchesLanguageFilter(ch, allowedLangs) {
			languageSkipped++
			return nil
		}
		index++
		dbChannels = append(dbChannels, &db.Channel{
			ID:        generateID(),
			LibraryID: libraryID,
			Name:      ch.Name,
			Number:    assignNumber(ch.Number, index),
			GroupName: ch.GroupName,
			LogoURL:   ch.LogoURL,
			StreamURL: ch.StreamURL,
			TvgID:     ch.TvgID,
			Language:  ch.Language,
			Country:   ch.Country,
			IsActive:  true,
			AddedAt:   now,
		})
		return nil
	})
	// Tolerate truncated downloads: large IPTV providers periodically
	// drop the connection mid-stream. If we already have a usable
	// count of live channels, prefer to commit them rather than lose
	// the whole import on a transport hiccup.
	if parseErr != nil {
		// Provider returned HTML / non-playlist content. Surface a
		// human-readable hint instead of the misleading "0 channels"
		// the caller would otherwise see — this is the single most
		// common failure mode for self-hosted IPTV (account suspended,
		// court-ordered IP block in ES, rate-limit, captive portal).
		if errors.Is(parseErr, ErrNotM3U) {
			s.logger.Error("M3U source did not return a playlist",
				"library", libraryID, "url", lib.M3UURL, "hint", "HTML/error page received")
			return 0, fmt.Errorf("the M3U URL returned an HTML page, not a playlist — "+
				"likely causes: account suspended, IP blocked (LaLiga/Movistar court "+
				"order in Spain), bad credentials, or rate-limit. Verify the URL in a "+
				"browser. Underlying: %w", parseErr)
		}
		const minUsable = 50
		if len(dbChannels) >= minUsable {
			s.logger.Warn("M3U parse truncated; importing what we got",
				"library", libraryID, "lines", parsedLines,
				"channels", len(dbChannels), "vod_skipped", vodSkipped,
				"language_skipped", languageSkipped, "error", parseErr)
		} else {
			return 0, fmt.Errorf("parse M3U: %w", parseErr)
		}
	} else {
		s.logger.Info("M3U parse complete",
			"library", libraryID, "lines", parsedLines,
			"channels", len(dbChannels), "vod_skipped", vodSkipped,
			"language_skipped", languageSkipped)
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
	if playlistEPGURL != "" && lib.EPGURL == "" {
		lib.EPGURL = playlistEPGURL
		lib.UpdatedAt = now
		if err := s.libraries.Update(ctx, lib); err != nil {
			// Don't fail the whole refresh — the channels are already saved,
			// and the EPG URL is nice-to-have. Log and move on.
			s.logger.Warn("persist discovered EPG URL",
				"library", libraryID, "epg_url", playlistEPGURL, "error", err)
		} else {
			epgDiscovered = true
			s.logger.Info("discovered EPG URL from playlist header",
				"library", libraryID, "epg_url", playlistEPGURL)
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

	// Trigger an active probe of the freshly-imported channels in
	// the background so dead upstreams move to the "Sin señal"
	// bucket without waiting for the periodic worker tick. Detached
	// ctx because probing hundreds of channels takes longer than
	// the HTTP refresh response should.
	if s.proberWorker != nil {
		go func(id string) {
			bg, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			if _, err := s.proberWorker.ProbeNow(bg, id); err != nil {
				s.logger.Warn("auto-probe after M3U import",
					"library", id, "error", err)
			}
		}(libraryID)
	}

	return len(dbChannels), nil
}

// parseLanguageFilter splits the comma-separated column value into
// the slice MatchesLanguageFilter expects. The library service
// already normalises codes to lowercase ISO on write, so this is a
// straight split + skip-empty.
func parseLanguageFilter(stored string) []string {
	if stored == "" {
		return nil
	}
	parts := strings.Split(stored, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
