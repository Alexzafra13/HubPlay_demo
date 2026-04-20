package iptv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	logger *slog.Logger,
) *Service {
	return &Service{
		channels:    channels,
		epgPrograms: epgPrograms,
		libraries:   libraries,
		favorites:   favorites,
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

	m3uChannels, err := ParseM3U(body)
	if err != nil {
		return 0, fmt.Errorf("parse M3U: %w", err)
	}

	now := time.Now()
	dbChannels := make([]*db.Channel, 0, len(m3uChannels))
	for i, ch := range m3uChannels {
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

	s.logger.Info("M3U refresh complete", "library", libraryID, "channels", len(dbChannels))
	s.publish(event.Event{
		Type: event.PlaylistRefreshed,
		Data: map[string]any{
			"library_id":     libraryID,
			"channels_count": len(dbChannels),
		},
	})
	return len(dbChannels), nil
}

// RefreshEPG downloads and parses an XMLTV EPG, updating program data.
//
// Uses the same per-library lock as RefreshM3U to stop two concurrent EPG
// refreshes from racing inside ReplaceForChannel (last-writer-wins on every
// channel, non-deterministically).
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

	if lib.EPGURL == "" {
		return 0, fmt.Errorf("library %s has no EPG URL configured", libraryID)
	}

	s.logger.Info("refreshing EPG", "library", libraryID, "url", lib.EPGURL)

	body, err := s.fetchURL(ctx, lib.EPGURL)
	if err != nil {
		return 0, fmt.Errorf("fetch EPG: %w", err)
	}
	defer body.Close() //nolint:errcheck

	epgData, err := ParseXMLTV(body)
	if err != nil {
		return 0, fmt.Errorf("parse XMLTV: %w", err)
	}

	// Get channels for this library to match EPG data
	channels, err := s.channels.ListByLibrary(ctx, libraryID, false)
	if err != nil {
		return 0, fmt.Errorf("list channels: %w", err)
	}

	// Build tvg-id → channel ID map
	tvgMap := make(map[string]string)
	nameMap := make(map[string]string)
	for _, ch := range channels {
		if ch.TvgID != "" {
			tvgMap[ch.TvgID] = ch.ID
		}
		nameMap[strings.ToLower(ch.Name)] = ch.ID
	}

	// Match EPG programs to channels and insert
	totalPrograms := 0
	programsByChannel := make(map[string][]*db.EPGProgram)

	for _, prog := range epgData.Programs {
		channelID := matchChannel(prog.ChannelID, tvgMap, nameMap)
		if channelID == "" {
			continue
		}

		programsByChannel[channelID] = append(programsByChannel[channelID], &db.EPGProgram{
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

	for channelID, programs := range programsByChannel {
		if err := s.epgPrograms.ReplaceForChannel(ctx, channelID, programs); err != nil {
			s.logger.Error("replace EPG programs", "channel", channelID, "error", err)
			continue
		}
		totalPrograms += len(programs)
	}

	s.logger.Info("EPG refresh complete", "library", libraryID, "programs", totalPrograms, "channels_matched", len(programsByChannel))
	s.publish(event.Event{
		Type: event.EPGUpdated,
		Data: map[string]any{
			"library_id":       libraryID,
			"programs_count":   totalPrograms,
			"channels_matched": len(programsByChannel),
		},
	})
	return totalPrograms, nil
}

// GetChannels returns channels for a library.
func (s *Service) GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*db.Channel, error) {
	return s.channels.ListByLibrary(ctx, libraryID, activeOnly)
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

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}

	return resp.Body, nil
}

// matchChannel tries to match an EPG channel ID to a database channel.
func matchChannel(epgChannelID string, tvgMap, nameMap map[string]string) string {
	// Exact tvg-id match
	if id, ok := tvgMap[epgChannelID]; ok {
		return id
	}
	// Case-insensitive name match
	if id, ok := nameMap[strings.ToLower(epgChannelID)]; ok {
		return id
	}
	return ""
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
