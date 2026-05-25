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

	"hubplay/internal/event"
	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
)

// TryAcquireRefresh reserves the per-library refresh slot synchronously
// and returns a release function the caller MUST defer. Returns
// ErrRefreshInProgress immediately if the slot is already held.
//
// Exposed so the HTTP handler can answer "is this kicking off or
// already running?" before deciding between 202 Accepted and 409
// Conflict, then continue the import in a goroutine that outlives the
// request — see iptv_admin.go RefreshM3U for the use site.
func (s *Service) TryAcquireRefresh(libraryID string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshes[libraryID] {
		return nil, fmt.Errorf("library %s: %w", libraryID, ErrRefreshInProgress)
	}
	s.refreshes[libraryID] = true
	return func() {
		s.mu.Lock()
		delete(s.refreshes, libraryID)
		s.mu.Unlock()
	}, nil
}

// RefreshM3U downloads and parses an M3U playlist, replacing channels for the library.
//
// Synchronous variant: acquires the lock, runs the import, releases.
// Used by the scheduler and the public-IPTV import path. The HTTP
// handler uses TryAcquireRefresh + RunRefreshM3U directly so it can
// 202-and-detach instead of holding the request open for minutes.
func (s *Service) RefreshM3U(ctx context.Context, libraryID string) (int, error) {
	release, err := s.TryAcquireRefresh(libraryID)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.RunRefreshM3U(ctx, libraryID)
}

// PublishRefreshFailed emits a playlist.refresh_failed SSE event so
// the admin UI can clear the spinner / show a toast when an async
// import gives up. The error message is forwarded verbatim — callers
// upstream of this method are responsible for not leaking provider
// secrets (the M3U URL itself never appears in domain errors).
func (s *Service) PublishRefreshFailed(libraryID string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.publish(event.Event{
		Type: event.PlaylistRefreshFailed,
		Data: map[string]any{
			"library_id": libraryID,
			"error":      msg,
		},
	})
}

// RunRefreshM3U performs the actual import. Caller is responsible for
// acquiring + releasing the per-library lock via TryAcquireRefresh.
// Exported (vs. RefreshM3U which is the lock-and-run wrapper) so the
// HTTP handler can drive the lock lifecycle from the request goroutine
// while the import runs detached.
func (s *Service) RunRefreshM3U(ctx context.Context, libraryID string) (int, error) {
	lib, err := s.libraries.GetByID(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("get library: %w", err)
	}
	if lib.M3UURL == "" {
		return 0, fmt.Errorf("library %s has no M3U URL configured", libraryID)
	}

	s.logger.Info("refreshing M3U playlist", "library", libraryID, "url", lib.M3UURL)

	// Pipeline: fetch+parse → persistir canales → descubrir EPG → publicar+background.
	result, err := s.fetchAndParseM3U(ctx, libraryID, lib.M3UURL, lib.TLSInsecure, lib.LanguageFilter)
	if err != nil {
		return 0, err
	}

	if err := s.persistChannels(ctx, libraryID, result.channels); err != nil {
		return 0, err
	}

	epgDiscovered := s.discoverEPGURL(ctx, lib, result.playlistEPGURL, result.now)

	s.publishAndTriggerBackground(libraryID, len(result.channels), epgDiscovered)

	return len(result.channels), nil
}

// m3uParseResult agrupa la salida del paso fetch+parse para pasar entre etapas.
type m3uParseResult struct {
	channels       []*iptvmodel.Channel
	playlistEPGURL string
	now            time.Time
}

// fetchAndParseM3U descarga la playlist M3U y la parsea en streaming,
// filtrando VOD y por idioma. Devuelve los canales válidos y la URL EPG
// descubierta en la cabecera (si la hay).
func (s *Service) fetchAndParseM3U(ctx context.Context, libraryID, m3uURL string, tlsInsecure bool, languageFilter string) (*m3uParseResult, error) {
	body, err := s.fetchURL(ctx, m3uURL, tlsInsecure)
	if err != nil {
		return nil, fmt.Errorf("fetch M3U: %w", err)
	}
	defer body.Close() //nolint:errcheck

	now := time.Now()
	var (
		dbChannels      []*iptvmodel.Channel
		index           int
		vodSkipped      int
		languageSkipped int
	)
	allowedLangs := parseLanguageFilter(languageFilter)

	playlistEPGURL, parsedLines, parseErr := ParseM3UStream(body, func(ch M3UChannel) error {
		if IsVODChannel(ch) {
			vodSkipped++
			return nil
		}
		if !MatchesLanguageFilter(ch, allowedLangs) {
			languageSkipped++
			return nil
		}
		index++
		dbChannels = append(dbChannels, &iptvmodel.Channel{
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

	if err := s.handleParseResult(libraryID, m3uURL, dbChannels, parsedLines, vodSkipped, languageSkipped, parseErr); err != nil {
		return nil, err
	}

	return &m3uParseResult{channels: dbChannels, playlistEPGURL: playlistEPGURL, now: now}, nil
}

// handleParseResult evalúa el error del parser y decide si el import
// puede continuar (descarga truncada con suficientes canales) o debe
// abortar (contenido no-M3U, muy pocos canales).
func (s *Service) handleParseResult(libraryID, m3uURL string, channels []*iptvmodel.Channel, parsedLines, vodSkipped, languageSkipped int, parseErr error) error {
	if parseErr == nil {
		s.logger.Info("M3U parse complete",
			"library", libraryID, "lines", parsedLines,
			"channels", len(channels), "vod_skipped", vodSkipped,
			"language_skipped", languageSkipped)
		return nil
	}

	// Contenido HTML / no-playlist: error descriptivo para el admin.
	if errors.Is(parseErr, ErrNotM3U) {
		s.logger.Error("M3U source did not return a playlist",
			"library", libraryID, "url", m3uURL, "hint", "HTML/error page received")
		return fmt.Errorf("the M3U URL returned an HTML page, not a playlist — "+
			"likely causes: account suspended, IP blocked (LaLiga/Movistar court "+
			"order in Spain), bad credentials, or rate-limit. Verify the URL in a "+
			"browser. Underlying: %w", parseErr)
	}

	// Descarga truncada: aceptar si hay suficientes canales útiles.
	const minUsable = 50
	if len(channels) >= minUsable {
		s.logger.Warn("M3U parse truncated; importing what we got",
			"library", libraryID, "lines", parsedLines,
			"channels", len(channels), "vod_skipped", vodSkipped,
			"language_skipped", languageSkipped, "error", parseErr)
		return nil
	}
	return fmt.Errorf("parse M3U: %w", parseErr)
}

// persistChannels reemplaza los canales de la biblioteca en la DB y
// re-aplica los overrides de admin (tvg_id, etc.) keyed por stream URL.
func (s *Service) persistChannels(ctx context.Context, libraryID string, channels []*iptvmodel.Channel) error {
	if err := s.channels.ReplaceForLibrary(ctx, libraryID, channels); err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}

	if s.overrides != nil {
		if n, err := s.overrides.ApplyToLibrary(ctx, libraryID); err != nil {
			s.logger.Error("apply channel overrides post-import",
				"library", libraryID, "error", err)
		} else if n > 0 {
			s.logger.Info("reapplied channel overrides",
				"library", libraryID, "count", n)
		}
	}
	return nil
}

// discoverEPGURL persiste la URL XMLTV anunciada en la cabecera del M3U
// si la biblioteca no tiene una configurada manualmente por el operador.
func (s *Service) discoverEPGURL(ctx context.Context, lib *librarymodel.Library, playlistEPGURL string, now time.Time) bool {
	if playlistEPGURL == "" || lib.EPGURL != "" {
		return false
	}

	lib.EPGURL = playlistEPGURL
	lib.UpdatedAt = now
	if err := s.libraries.Update(ctx, lib); err != nil {
		s.logger.Warn("persist discovered EPG URL",
			"library", lib.ID, "epg_url", playlistEPGURL, "error", err)
		return false
	}
	s.logger.Info("discovered EPG URL from playlist header",
		"library", lib.ID, "epg_url", playlistEPGURL)
	return true
}

// publishAndTriggerBackground emite el evento SSE de refresh completado
// y lanza las goroutines de EPG auto-refresh y probe post-import.
func (s *Service) publishAndTriggerBackground(libraryID string, channelCount int, epgDiscovered bool) {
	s.logger.Info("M3U refresh complete", "library", libraryID, "channels", channelCount)
	s.publish(event.Event{
		Type: event.PlaylistRefreshed,
		Data: map[string]any{
			"library_id":     libraryID,
			"channels_count": channelCount,
		},
	})

	// EPG refresh para URLs recién descubiertas, en background.
	if epgDiscovered {
		id := libraryID
		s.SpawnBackground(func(bgCtx context.Context) {
			ctx, cancel := context.WithTimeout(bgCtx, 5*time.Minute)
			defer cancel()
			if _, err := s.RefreshEPG(ctx, id); err != nil {
				s.logger.Warn("auto-trigger EPG refresh after M3U import",
					"library", id, "error", err)
			}
		})
	}

	// Probe activo de canales importados para detectar upstreams muertos.
	if s.proberWorker != nil {
		id := libraryID
		s.SpawnBackground(func(bgCtx context.Context) {
			ctx, cancel := context.WithTimeout(bgCtx, 30*time.Minute)
			defer cancel()
			if _, err := s.proberWorker.ProbeNow(ctx, id); err != nil {
				s.logger.Warn("auto-probe after M3U import",
					"library", id, "error", err)
			}
		})
	}
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
