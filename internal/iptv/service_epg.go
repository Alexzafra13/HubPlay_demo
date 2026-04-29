package iptv

// EPG refresh + query surface. Multi-source merge (priority-owns) with
// per-source status recording, plus the fuzzy matcher that joins
// free-community XMLTV feeds (davidmuma, epg.pw) to the iptv-org M3U.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// RefreshEPG downloads every configured XMLTV source for a library,
// merges them by priority, and replaces the persisted EPG.
//
// Merge semantics: sources are processed in ascending priority order.
// The first source that matches a channel OWNS that channel — lower-
// priority sources may not overwrite its programmes. Channels the
// first source doesn't cover are still available to the next one.
// Concretely: point priority 0 at davidmuma for cadenas grandes, point
// priority 1 at epg.pw for the long tail, and every channel gets the
// best guide either can provide without fighting the other.
//
// Error handling is per-source: a 404 on davidmuma no longer aborts
// the whole refresh; it's recorded against that source and epg.pw
// still runs. The function only returns an error if every source
// failed (so the admin sees an "all sources broken" signal rather
// than a silent partial).
//
// Back-compat: if the library has no rows in library_epg_sources but
// the legacy `libraries.epg_url` is set, we treat that column as a
// single implicit priority-0 custom source. Migration 007 already
// copies the column into the table on upgrade, so this path only
// triggers for libraries created on an older build that never wrote
// the column itself.
//
// Uses the same per-library lock as RefreshM3U to stop two concurrent
// EPG refreshes from racing inside ReplaceForChannel.
func (s *Service) RefreshEPG(ctx context.Context, libraryID string) (int, error) {
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

	sources, err := s.epgSources.ListByLibrary(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("list epg sources: %w", err)
	}
	if len(sources) == 0 && lib.EPGURL != "" {
		// Legacy single-URL library: synthesise a transient source so
		// the merge path still runs. We do NOT persist this row —
		// migration 007 does that on upgrade. Only in-memory fallback
		// to cover "operator edits the column directly after upgrade".
		sources = []*db.LibraryEPGSource{{
			ID: "", LibraryID: libraryID, URL: lib.EPGURL, Priority: 0,
		}}
	}
	if len(sources) == 0 {
		return 0, fmt.Errorf("library %s has no EPG sources configured", libraryID)
	}

	// Cross-library matching: build the channel index from EVERY
	// livetv channel on the instance, not just the library that
	// owns the EPG sources. The realistic operator setup is "one
	// M3U for cable, one for IPTV provider, but only one set of
	// EPG XML sources": with per-library indexes, the second
	// library would show "Sin guía" on every card it could
	// otherwise match by name / tvg-id.
	//
	// Safety: matchChannel still returns at most one channel id per
	// XMLTV channel (the closest match by the same priority order:
	// tvg-id → name variants → fuzzy). The persistence loop later
	// only ReplaceForChannel's channels we matched, so libraries
	// that didn't match anything keep whatever EPG they already
	// had — no surprise wipes.
	channels, err := s.channels.ListLivetvChannels(ctx)
	if err != nil {
		return 0, fmt.Errorf("list livetv channels: %w", err)
	}
	idx := buildChannelIndex(channels)
	// Channel-id → library-id lookup so the persistence loop below
	// can attribute each replaced program back to a library, and the
	// summary log lists "how many channels in library X did this
	// refresh cover". Load-bearing for the "is my EPG actually
	// reaching the channels I expect" debugging loop.
	channelLib := make(map[string]string, len(channels))
	for _, c := range channels {
		channelLib[c.ID] = c.LibraryID
	}

	// Merge accumulators — persist once at the end so a per-source
	// failure doesn't leave the DB half-populated.
	ownedByChannel := make(map[string][]*db.EPGProgram)
	totalOrphans := 0
	workedCount := 0

	for _, src := range sources {
		progs, matched, orphans, fetchErr := s.refreshOneSource(ctx, src, idx, ownedByChannel, lib.TLSInsecure)
		totalOrphans += orphans
		if fetchErr != nil {
			s.logger.Warn("EPG source failed",
				"library", libraryID, "url", src.URL, "error", fetchErr)
			if src.ID != "" {
				if rerr := s.epgSources.RecordRefresh(ctx, src.ID, "error", fetchErr.Error(), 0, 0); rerr != nil {
					s.logger.Error("record source error", "source", src.ID, "error", rerr)
				}
			}
			continue
		}
		workedCount++

		for chID, list := range progs {
			ownedByChannel[chID] = list
		}
		progCount := 0
		for _, list := range progs {
			progCount += len(list)
		}
		if src.ID != "" {
			if rerr := s.epgSources.RecordRefresh(ctx, src.ID, "ok", "", progCount, matched); rerr != nil {
				s.logger.Error("record source ok", "source", src.ID, "error", rerr)
			}
		}
		s.logger.Info("EPG source loaded",
			"library", libraryID, "url", src.URL,
			"programs", progCount, "channels_matched", matched)
	}

	if workedCount == 0 {
		return 0, fmt.Errorf("all EPG sources failed for library %s", libraryID)
	}

	// Persist the merged EPG. One ReplaceForChannel per covered channel
	// — channels not present in any source keep their previous data
	// (safer than blanking them whenever a single source hiccups).
	totalPrograms := 0
	matchedByLib := make(map[string]int)
	for channelID, programs := range ownedByChannel {
		if err := s.epgPrograms.ReplaceForChannel(ctx, channelID, programs); err != nil {
			s.logger.Error("replace EPG programs", "channel", channelID, "error", err)
			continue
		}
		totalPrograms += len(programs)
		matchedByLib[channelLib[channelID]]++
	}

	// Count cross-library matches (channels in a library other than
	// the one whose sources were just refreshed). This is the metric
	// that proves the cross-library matching is doing real work; if
	// it's always 0, the operator's M3Us don't share tvg-id / names.
	crossLibMatches := 0
	for libID, count := range matchedByLib {
		if libID != libraryID {
			crossLibMatches += count
		}
	}
	s.logger.Info("EPG refresh complete",
		"library", libraryID,
		"programs", totalPrograms,
		"channels_matched", len(ownedByChannel),
		"channels_matched_by_lib", matchedByLib,
		"channels_matched_cross_library", crossLibMatches,
		"orphan_programs", totalOrphans,
		"sources_ok", workedCount,
		"sources_total", len(sources))
	s.publish(event.Event{
		Type: event.EPGUpdated,
		Data: map[string]any{
			"library_id":       libraryID,
			"programs_count":   totalPrograms,
			"channels_matched": len(ownedByChannel),
			"orphan_programs":  totalOrphans,
			"sources_ok":       workedCount,
			"sources_total":    len(sources),
		},
	})
	return totalPrograms, nil
}

// refreshOneSource fetches a single XMLTV URL and matches its
// programmes against the channel lookups. Programmes for channels
// already owned by a higher-priority source are skipped so the merge
// caller can just assign into ownedByChannel without worrying about
// precedence.
//
// Returns: the per-channel program map this source contributes, the
// number of channels matched, the number of orphan programmes (no
// channel match), and any fetch/parse error.
//
// Resolution is cached per XMLTV channel id so the 7 k programmes a
// davidmuma dump ships don't each walk the fuzzy pool. Programmes
// referencing ids missing from <channel> blocks fall through to a
// lazy resolve.
func (s *Service) refreshOneSource(
	ctx context.Context,
	src *db.LibraryEPGSource,
	idx *channelIndex,
	alreadyOwned map[string][]*db.EPGProgram,
	tlsInsecure bool,
) (map[string][]*db.EPGProgram, int, int, error) {
	body, err := s.fetchURL(ctx, src.URL, tlsInsecure)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("fetch EPG: %w", err)
	}
	defer body.Close() //nolint:errcheck

	// Streaming parse: never holds the full feed in memory. The
	// matchHandler buffers <channel> defs for the resolution
	// cache (small, ~few thousand × KB) and dispatches programmes
	// one at a time, dropping orphans without keeping their
	// payload. Memory cap is therefore O(matched programmes ×
	// programme size), not O(feed size).
	mh := newMatchHandler(idx, alreadyOwned)
	if _, err := ParseXMLTVStream(body, mh); err != nil {
		// Provider returned HTML / non-XMLTV content — same failure
		// shape as the M3U side (account suspended, IP blocked by
		// LaLiga/Movistar court order in ES, captive portal,
		// rate-limit). Surface a useful hint instead of a generic
		// XML decode error.
		if errors.Is(err, ErrNotXMLTV) {
			s.logger.Error("EPG source did not return an XMLTV document",
				"hint", "HTML/error page received")
			return nil, 0, 0, fmt.Errorf("the EPG URL returned an HTML page, not XMLTV — "+
				"likely causes: account suspended, IP blocked (LaLiga/Movistar court "+
				"order in Spain), bad credentials, or rate-limit. Verify the URL in a "+
				"browser. Underlying: %w", err)
		}
		return nil, 0, 0, fmt.Errorf("parse XMLTV: %w", err)
	}
	return mh.out, len(mh.out), mh.orphans, nil
}

// matchHandler is the EPGStreamHandler used by refreshOneSource. It
// owns the resolution cache + per-channel programme accumulator so
// the parser can stay memory-bounded.
//
// First-programme cold start: when the first <programme> arrives, we
// walk every accumulated <channel> through matchChannel once and
// build the cache. Programmes referencing channel ids that weren't
// declared in a <channel> block fall through to a lazy match that
// memoises into the same cache.
type matchHandler struct {
	idx          *channelIndex
	alreadyOwned map[string][]*db.EPGProgram
	candidates   map[string][]string // xmltv channel id → display-name aliases
	resolved     map[string]string   // xmltv channel id → hub channel id ("" = no match)
	cacheArmed   bool
	out          map[string][]*db.EPGProgram
	orphans      int
}

func newMatchHandler(idx *channelIndex, alreadyOwned map[string][]*db.EPGProgram) *matchHandler {
	return &matchHandler{
		idx:          idx,
		alreadyOwned: alreadyOwned,
		candidates:   make(map[string][]string),
		resolved:     make(map[string]string),
		out:          make(map[string][]*db.EPGProgram),
	}
}

func (m *matchHandler) OnChannel(ch EPGChannel) error {
	names := make([]string, 0, 1+len(ch.DisplayNames))
	names = append(names, ch.ID)
	names = append(names, ch.DisplayNames...)
	m.candidates[ch.ID] = names
	// Don't pre-resolve here — for very large feeds this would do
	// thousands of fuzzy walks before the first programme arrives.
	// The cold-start path below batches it on first programme so we
	// still pay it once, but only if the feed actually has any
	// programmes (not all do).
	return nil
}

func (m *matchHandler) OnProgramme(p EPGProgram) error {
	if !m.cacheArmed {
		// Cold-start: resolve every declared XMLTV channel id once.
		// Same optimisation as the eager parser had; keeping the same
		// cost profile means the matcher's per-source budget doesn't
		// change with this refactor.
		for id, names := range m.candidates {
			m.resolved[id] = matchChannel(id, names, m.idx)
		}
		m.cacheArmed = true
	}

	channelID, cached := m.resolved[p.ChannelID]
	if !cached {
		// Programme references a channel id that wasn't declared in
		// a <channel> block. Resolve with the id alone and memoise.
		// Bounded by the number of distinct undeclared ids; fuzzy's
		// O(pool) cost stays in-budget for any sane feed.
		channelID = matchChannel(p.ChannelID, nil, m.idx)
		m.resolved[p.ChannelID] = channelID
	}
	if channelID == "" {
		m.orphans++
		return nil
	}
	if _, owned := m.alreadyOwned[channelID]; owned {
		// A higher-priority source already covered this channel.
		return nil
	}
	m.out[channelID] = append(m.out[channelID], &db.EPGProgram{
		ID:          generateID(),
		ChannelID:   channelID,
		Title:       p.Title,
		Description: p.Description,
		Category:    p.Category,
		IconURL:     p.IconURL,
		StartTime:   p.Start,
		EndTime:     p.Stop,
	})
	return nil
}

// ── EPG query surface (read-side) ─────────────────────────────────

// GetSchedule returns EPG programs for a channel within a time range.
func (s *Service) GetSchedule(ctx context.Context, channelID string, from, to time.Time) ([]*db.EPGProgram, error) {
	return s.epgPrograms.Schedule(ctx, channelID, from, to)
}

// GetBulkSchedule returns EPG programs for multiple channels.
func (s *Service) GetBulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*db.EPGProgram, error) {
	return s.epgPrograms.BulkSchedule(ctx, channelIDs, from, to)
}

// NowPlaying returns the currently airing program for a channel.
func (s *Service) NowPlaying(ctx context.Context, channelID string) (*db.EPGProgram, error) {
	return s.epgPrograms.NowPlaying(ctx, channelID)
}

// CleanupOldPrograms removes EPG data older than 24 h. Called by the
// scheduled cleanup job so stale programmes don't grow the DB
// indefinitely.
func (s *Service) CleanupOldPrograms(ctx context.Context) (int64, error) {
	before := time.Now().Add(-24 * time.Hour)
	return s.epgPrograms.CleanupOld(ctx, before)
}

// The channel ↔ XMLTV matcher lives in matcher.go. service_epg.go
// only consumes it via buildChannelIndex + matchChannel above.
