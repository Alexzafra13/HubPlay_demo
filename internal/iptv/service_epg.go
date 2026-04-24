package iptv

// EPG refresh + query surface. Multi-source merge (priority-owns) with
// per-source status recording, plus the fuzzy matcher that joins
// free-community XMLTV feeds (davidmuma, epg.pw) to the iptv-org M3U.

import (
	"context"
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

	channels, err := s.channels.ListByLibrary(ctx, libraryID, false)
	if err != nil {
		return 0, fmt.Errorf("list channels: %w", err)
	}
	idx := buildChannelIndex(channels)

	// Merge accumulators — persist once at the end so a per-source
	// failure doesn't leave the DB half-populated.
	ownedByChannel := make(map[string][]*db.EPGProgram)
	totalOrphans := 0
	workedCount := 0

	for _, src := range sources {
		progs, matched, orphans, fetchErr := s.refreshOneSource(ctx, src, idx, ownedByChannel)
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
	for channelID, programs := range ownedByChannel {
		if err := s.epgPrograms.ReplaceForChannel(ctx, channelID, programs); err != nil {
			s.logger.Error("replace EPG programs", "channel", channelID, "error", err)
			continue
		}
		totalPrograms += len(programs)
	}

	s.logger.Info("EPG refresh complete",
		"library", libraryID,
		"programs", totalPrograms,
		"channels_matched", len(ownedByChannel),
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
) (map[string][]*db.EPGProgram, int, int, error) {
	body, err := s.fetchURL(ctx, src.URL)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("fetch EPG: %w", err)
	}
	defer body.Close() //nolint:errcheck

	epgData, err := ParseXMLTV(body)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse XMLTV: %w", err)
	}

	// Pre-compute <channel>-level resolution so programme iteration is a
	// map lookup. Non-matches cache as "" so we don't re-try every time.
	resolved := make(map[string]string, len(epgData.Channels))
	xmltvCandidates := make(map[string][]string, len(epgData.Channels))
	for _, xch := range epgData.Channels {
		names := make([]string, 0, 1+len(xch.DisplayNames))
		names = append(names, xch.ID)
		names = append(names, xch.DisplayNames...)
		xmltvCandidates[xch.ID] = names
		resolved[xch.ID] = matchChannel(xch.ID, names, idx)
	}

	out := make(map[string][]*db.EPGProgram)
	orphans := 0
	for _, prog := range epgData.Programs {
		channelID, cached := resolved[prog.ChannelID]
		if !cached {
			// Programme references a channel id that wasn't declared
			// in a <channel> block — resolve with the id alone and
			// memoise so repeats don't re-match.
			channelID = matchChannel(prog.ChannelID, nil, idx)
			resolved[prog.ChannelID] = channelID
		}
		if channelID == "" {
			orphans++
			continue
		}
		if _, owned := alreadyOwned[channelID]; owned {
			// A higher-priority source already covered this channel.
			continue
		}
		out[channelID] = append(out[channelID], &db.EPGProgram{
			ID:          generateID(),
			ChannelID:   channelID,
			Title:       prog.Title,
			Description: prog.Description,
			Category:    prog.Category,
			IconURL:     prog.IconURL,
			StartTime:   prog.Start,
			EndTime:     prog.Stop,
		})
	}
	return out, len(out), orphans, nil
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
