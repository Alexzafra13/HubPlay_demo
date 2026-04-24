package iptv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"bufio"
	"compress/gzip"
	"regexp"
	"strings"
	"sync"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// Service manages IPTV libraries: M3U import, EPG sync, channel operations.
type Service struct {
	channels    *db.ChannelRepository
	epgPrograms *db.EPGProgramRepository
	libraries   *db.LibraryRepository
	favorites   *db.ChannelFavoritesRepository
	epgSources  *db.LibraryEPGSourceRepository
	overrides   *db.ChannelOverrideRepository
	logger      *slog.Logger

	mu        sync.Mutex
	refreshes map[string]bool // tracks ongoing refreshes by library ID

	httpClient *http.Client
	stopCh     chan struct{}

	bus *event.Bus // optional; nil-safe
}

// SetEventBus wires an event bus so the service publishes PlaylistRefreshed
// / EPGUpdated events at the end of the respective refresh. Nil-safe.
func (s *Service) SetEventBus(bus *event.Bus) { s.bus = bus }

func (s *Service) publish(e event.Event) {
	if s.bus != nil {
		s.bus.Publish(e)
	}
}

// NewService creates a new IPTV service.
func NewService(
	channels *db.ChannelRepository,
	epgPrograms *db.EPGProgramRepository,
	libraries *db.LibraryRepository,
	favorites *db.ChannelFavoritesRepository,
	epgSources *db.LibraryEPGSourceRepository,
	overrides *db.ChannelOverrideRepository,
	logger *slog.Logger,
) *Service {
	return &Service{
		channels:    channels,
		epgPrograms: epgPrograms,
		libraries:   libraries,
		favorites:   favorites,
		epgSources:  epgSources,
		overrides:   overrides,
		logger:      logger.With("module", "iptv"),
		refreshes:   make(map[string]bool),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

// ── Channel favorites ─────────────────────────────────────────────
//
// Favorites are a per-user overlay on top of the (library-scoped) channel
// catalog. All methods require the caller to have already authenticated the
// user and to have verified the channel belongs to a library the user can
// access — the handler layer does that before reaching here.

// AddFavorite marks a channel as favorited by a user. Idempotent.
func (s *Service) AddFavorite(ctx context.Context, userID, channelID string) error {
	return s.favorites.Add(ctx, userID, channelID)
}

// RemoveFavorite unmarks a channel as favorited by a user. Idempotent.
func (s *Service) RemoveFavorite(ctx context.Context, userID, channelID string) error {
	return s.favorites.Remove(ctx, userID, channelID)
}

// IsFavorite reports whether a channel is currently favorited by a user.
func (s *Service) IsFavorite(ctx context.Context, userID, channelID string) (bool, error) {
	return s.favorites.Contains(ctx, userID, channelID)
}

// ListFavoriteIDs returns the user's favorite channel IDs (most-recent first).
// Cheap: one indexed query, no JOIN. Use when the client already has the
// channel list and just needs to toggle ♥ state.
func (s *Service) ListFavoriteIDs(ctx context.Context, userID string) ([]string, error) {
	return s.favorites.ListIDs(ctx, userID)
}

// ListFavoriteChannels returns the user's favorite channels with full channel
// data. Filters out inactive channels — a favorited channel that later went
// dark shouldn't surface as a dead card.
func (s *Service) ListFavoriteChannels(ctx context.Context, userID string) ([]*db.Channel, error) {
	return s.favorites.ListChannels(ctx, userID)
}

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
	tvgMap, nameMap := buildChannelLookups(channels)

	// Merge accumulators — persist once at the end so a per-source
	// failure doesn't leave the DB half-populated.
	ownedByChannel := make(map[string][]*db.EPGProgram)
	totalOrphans := 0
	workedCount := 0

	for _, src := range sources {
		progs, matched, orphans, fetchErr := s.refreshOneSource(ctx, src, tvgMap, nameMap, ownedByChannel)
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

// buildChannelLookups prepares the tvg-id and normalised-name maps
// the XMLTV matcher walks on every programme. Extracted from the old
// RefreshEPG so the multi-source loop can reuse them without rebuilding
// per source.
//
// Common scenario addressed: iptv-org M3U with tvg-ids like
// "3CatInfo.es@SD" paired with a Spanish community EPG (davidmuma,
// epg.pw) that uses readable names like "3CatInfo" or "La 1 HD".
// Without the name map the EPG loads but never joins.
func buildChannelLookups(channels []*db.Channel) (tvgMap, nameMap map[string]string) {
	tvgMap = make(map[string]string, len(channels))
	nameMap = make(map[string]string, len(channels)*2)
	for _, ch := range channels {
		if ch.TvgID != "" {
			tvgMap[ch.TvgID] = ch.ID
		}
		for _, v := range nameVariants(ch.Name) {
			if _, exists := nameMap[v]; !exists {
				nameMap[v] = ch.ID
			}
		}
	}
	return tvgMap, nameMap
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
func (s *Service) refreshOneSource(
	ctx context.Context,
	src *db.LibraryEPGSource,
	tvgMap, nameMap map[string]string,
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

	xmltvCandidates := make(map[string][]string, len(epgData.Channels))
	for _, xch := range epgData.Channels {
		names := make([]string, 0, 1+len(xch.DisplayNames))
		names = append(names, xch.ID)
		names = append(names, xch.DisplayNames...)
		xmltvCandidates[xch.ID] = names
	}

	out := make(map[string][]*db.EPGProgram)
	orphans := 0
	for _, prog := range epgData.Programs {
		channelID := matchChannel(prog.ChannelID, xmltvCandidates[prog.ChannelID], tvgMap, nameMap)
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

// GetChannels returns channels for a library.
//
// When activeOnly is true (the default for user-facing surfaces) the
// list is also filtered for health: channels that have failed the
// probe UnhealthyThreshold times in a row are hidden so viewers
// don't click dead tiles. Admin callers pass activeOnly=false and
// get the full set including disabled and unhealthy rows.
func (s *Service) GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*db.Channel, error) {
	if activeOnly {
		return s.channels.ListHealthyByLibrary(ctx, libraryID)
	}
	return s.channels.ListByLibrary(ctx, libraryID, false)
}

// GetChannel returns a single channel by ID.
func (s *Service) GetChannel(ctx context.Context, id string) (*db.Channel, error) {
	return s.channels.GetByID(ctx, id)
}

// GetGroups returns channel group names for a library.
func (s *Service) GetGroups(ctx context.Context, libraryID string) ([]string, error) {
	return s.channels.Groups(ctx, libraryID)
}

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

// SetChannelActive enables or disables a channel.
func (s *Service) SetChannelActive(ctx context.Context, id string, active bool) error {
	return s.channels.SetActive(ctx, id, active)
}

// ── Channel health reporter ───────────────────────────────────────
//
// Wraps the channel repo so the stream proxy can persist probe
// outcomes without importing `db`. Each call is fire-and-forget from
// the proxy's point of view: we use a short-deadline background ctx
// so the DB write never outlives its reasonable bound even if the
// caller's ctx is about to expire, and we don't surface DB errors
// upward — a failed health write must not tear down a stream.

// channelHealthProbeTimeout caps how long the repo has to record one
// probe outcome. Keeping the upper bound tight stops a slow SQLite
// WAL flush from pinning the proxy's goroutine across several seconds
// of latency noise.
const channelHealthProbeTimeout = 2 * time.Second

// RecordProbeSuccess satisfies iptv.ChannelHealthReporter. Logs on
// DB error and moves on — the stream proxy does not care whether we
// actually persisted the state.
func (s *Service) RecordProbeSuccess(ctx context.Context, channelID string) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeSuccess(ctx, channelID); err != nil {
		s.logger.Debug("record probe success", "channel", channelID, "error", err)
	}
}

// RecordProbeFailure satisfies iptv.ChannelHealthReporter. Translates
// the Go error into a short, user-safe string before persisting.
func (s *Service) RecordProbeFailure(ctx context.Context, channelID string, probeErr error) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeFailure(ctx, channelID, sanitiseProbeError(probeErr)); err != nil {
		s.logger.Debug("record probe failure", "channel", channelID, "error", err)
	}
}

// sanitiseProbeError trims repeated prefix wrapping and strips any
// obviously-transient noise so the admin UI shows the underlying
// cause ("no such host", "connection refused", "HTTP 403") rather
// than a nest of wrapper prefixes.
func sanitiseProbeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Strip our own wrapping prefixes ("connect: ", "fetch EPG: ") so
	// the useful underlying message is what lands in the DB.
	for _, prefix := range []string{"connect: ", "fetch: ", "read upstream: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// ── Manual channel editing (admin) ─────────────────────────────────
//
// Surface for the "canales sin guía" panel. The admin patches a
// channel's tvg_id to force a match against a specific XMLTV entry
// (e.g. davidmuma uses "La 1 HD" where the iptv-org M3U has
// "La1.es@SD"). The change is applied immediately to `channels` AND
// persisted to `channel_overrides` so it survives the next M3U refresh.

// ChannelWithoutEPGWindow is how far forward the "does this channel
// have a guide" check looks. 24h matches the default guide window on
// the user side.
const ChannelWithoutEPGWindow = 24 * time.Hour

// ListChannelsWithoutEPG returns active channels in the library that
// have no programmes overlapping the next ChannelWithoutEPGWindow.
// Admin-only use: surfaces the long tail of channels that the EPG
// match didn't cover.
func (s *Service) ListChannelsWithoutEPG(ctx context.Context, libraryID string) ([]*db.Channel, error) {
	now := time.Now().UTC()
	return s.channels.ListWithoutEPGByLibrary(ctx, libraryID,
		now.Add(-2*time.Hour), now.Add(ChannelWithoutEPGWindow))
}

// SetChannelTvgID updates one channel's tvg_id in place and records
// the edit in the overrides table so it replays on the next M3U
// refresh. Passing an empty tvgID clears both the column and the
// override row (the admin wants "use no tvg_id, let name matching
// carry the load").
func (s *Service) SetChannelTvgID(ctx context.Context, channelID, tvgID string) error {
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	if err := s.channels.UpdateTvgID(ctx, channelID, tvgID); err != nil {
		return fmt.Errorf("update tvg_id: %w", err)
	}
	if s.overrides == nil {
		return nil
	}
	if tvgID == "" {
		// Clear the persistent override so a future M3U refresh doesn't
		// resurrect a value the admin just removed.
		if err := s.overrides.Delete(ctx, ch.LibraryID, ch.StreamURL); err != nil {
			return fmt.Errorf("clear override: %w", err)
		}
		return nil
	}
	return s.overrides.Upsert(ctx, &db.ChannelOverride{
		LibraryID: ch.LibraryID,
		StreamURL: ch.StreamURL,
		TvgID:     tvgID,
	})
}

// ListUnhealthyChannels returns channels whose consecutive-failure
// count is at or above the threshold. Threshold 0 uses the repo
// default (db.UnhealthyThreshold).
func (s *Service) ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*db.Channel, error) {
	return s.channels.ListUnhealthyByLibrary(ctx, libraryID, threshold)
}

// ResetChannelHealth clears the health state for one channel so it
// reappears in the user-facing list on next render. Used by the
// admin "marcar como OK" action.
func (s *Service) ResetChannelHealth(ctx context.Context, channelID string) error {
	return s.channels.ResetHealth(ctx, channelID)
}

// ── EPG source management ─────────────────────────────────────────
//
// Admin-facing surface for the multi-provider EPG model. The handler
// layer gates these behind the admin role; the service itself only
// validates shape and catalog integrity.

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

// CleanupOldPrograms removes EPG data older than the given duration.
func (s *Service) CleanupOldPrograms(ctx context.Context) (int64, error) {
	before := time.Now().Add(-24 * time.Hour)
	return s.epgPrograms.CleanupOld(ctx, before)
}

// Shutdown stops any background processes.
func (s *Service) Shutdown() {
	close(s.stopCh)
}

// fetchURL downloads content from a URL.
func (s *Service) fetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	// Accept-Encoding: most EPG hosts publish a `.xml.gz` URL and expect
	// the client to gunzip. Some also negotiate via Content-Encoding.
	// We handle both: see maybeDecompress below.
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}

	return maybeDecompress(resp.Body, resp.Header.Get("Content-Encoding"), url)
}

// maybeDecompress returns a reader that transparently gunzips the body when
// it's gzipped. Detection uses three signals in order of reliability:
//
//  1. Content-Encoding header explicitly says "gzip" (standard HTTP).
//  2. URL ends in ".gz" (common for static hosts serving pre-gzipped files
//     with Content-Type: application/x-gzip and no Content-Encoding header —
//     GitHub raw does exactly this).
//  3. The first two bytes match the gzip magic (1f 8b). This catches hosts
//     that mis-serve `.xml` URLs as gzip bytes.
//
// Falls back to the raw body if nothing matches — never blows up a refresh
// because of detection uncertainty.
func maybeDecompress(body io.ReadCloser, contentEncoding, url string) (io.ReadCloser, error) {
	if strings.EqualFold(contentEncoding, "gzip") || strings.HasSuffix(strings.ToLower(url), ".gz") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("gunzip %s: %w", url, err)
		}
		return &gzipCloser{Reader: gz, underlying: body}, nil
	}

	// Sniff magic bytes as a last resort — wrap with a bufio peek that
	// doesn't lose data.
	br := bufio.NewReader(body)
	peek, _ := br.Peek(2)
	if len(peek) == 2 && peek[0] == 0x1f && peek[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("gunzip %s: %w", url, err)
		}
		return &gzipCloser{Reader: gz, underlying: body}, nil
	}
	return &bufferedCloser{Reader: br, underlying: body}, nil
}

// gzipCloser closes both the gzip reader and the underlying HTTP body.
// The stdlib gzip.Reader doesn't chain Close() to its source.
type gzipCloser struct {
	io.Reader
	underlying io.Closer
}

func (g *gzipCloser) Close() error {
	if closer, ok := g.Reader.(io.Closer); ok {
		_ = closer.Close()
	}
	return g.underlying.Close()
}

// bufferedCloser wraps a bufio.Reader so its Close() reaches the underlying
// http.Response.Body.
type bufferedCloser struct {
	io.Reader
	underlying io.Closer
}

func (b *bufferedCloser) Close() error { return b.underlying.Close() }

// matchChannel tries to match an EPG channel ID to a database channel.
// matchChannel joins an XMLTV programme to one of our channels. Tries, in
// order: exact tvg-id match, exact tvg-id match against any XMLTV
// display-name alias, name-variant match against the programme's XMLTV
// channel id, name-variant match against any display-name alias.
//
// The goal is to make free community EPGs (davidmuma, epg.pw, …) join up
// with the iptv-org M3U even though their channel IDs don't align. Once
// one variant matches we keep that binding — no scoring / "best match"
// heuristic, because XMLTV display-name lists are curated enough that
// any match is reliable.
func matchChannel(epgChannelID string, xmltvDisplayNames []string, tvgMap, nameMap map[string]string) string {
	// 1. Exact tvg-id on the incoming programme's channel ref.
	if id, ok := tvgMap[epgChannelID]; ok {
		return id
	}
	// 2. Some EPGs expose each channel's stream URL as tvg-id in their
	//    M3U pair; try XMLTV display-names against tvgMap too.
	for _, dn := range xmltvDisplayNames {
		if id, ok := tvgMap[dn]; ok {
			return id
		}
	}
	// 3. Normalised-name match against the XMLTV channel id itself.
	for _, v := range nameVariants(epgChannelID) {
		if id, ok := nameMap[v]; ok {
			return id
		}
	}
	// 4. Normalised-name match against every display-name alias. This is
	//    where davidmuma's `<display-name>La 1 HD</display-name>` hooks
	//    into our channel.Name = "La 1 (1080p)" after stripping quality.
	for _, dn := range xmltvDisplayNames {
		for _, v := range nameVariants(dn) {
			if id, ok := nameMap[v]; ok {
				return id
			}
		}
	}
	return ""
}

// qualityRE matches the quality / resolution / bitrate suffixes that
// iptv-org and other sources routinely append to channel names. Kept as
// a list of alternations rather than a single regex so we can extend it
// with provider-specific noise (e.g. "[Geo-blocked]", "[Not 24/7]").
var qualityRE = regexp.MustCompile(
	`(?i)\s*(?:\[[^\]]*\]|\([^)]*\)|\b(?:uhd|fhd|hd|sd|4k|8k|1080p?|720p?|576p?|480p?|360p?|240p?|backup|alt)\b)`,
)

// nameVariants returns a list of lowercased, accent-folded strings that
// should all match the same channel. For "La 1 (1080p) [Geo-blocked]" it
// yields ["la 1 (1080p) [geo-blocked]", "la 1"]. The fully-stripped
// variant is what usually matches EPG display-names.
//
// Whitespace is always collapsed in both variants: iptv-org feeds
// routinely carry doubled or trailing spaces ("  Canal  Sur  "), and
// treating those as distinct from the cleaned form would create a
// spurious second variant that never matches anything real.
func nameVariants(name string) []string {
	base := strings.ToLower(strings.TrimSpace(name))
	if base == "" {
		return nil
	}
	folded := diacriticFolder.Replace(base)
	folded = strings.Join(strings.Fields(folded), " ")
	variants := []string{folded}

	stripped := strings.TrimSpace(qualityRE.ReplaceAllString(folded, " "))
	stripped = strings.Join(strings.Fields(stripped), " ")
	if stripped != "" && stripped != folded {
		variants = append(variants, stripped)
	}
	return variants
}

func assignNumber(parsed, index int) int {
	if parsed > 0 {
		return parsed
	}
	return index
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
