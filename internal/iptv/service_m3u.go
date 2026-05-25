package iptv

// Importación M3U — descarga la playlist, reemplaza los canales de la
// biblioteca y re-aplica overrides del admin. Si la playlist anuncia
// un `url-tvg`, dispara un refresh EPG oportunista.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/event"
)

// TryAcquireRefresh reserva el slot de refresh per-library y devuelve
// una función de liberación que el caller DEBE defer. Devuelve
// ErrRefreshInProgress si el slot ya está ocupado.
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

// RefreshM3U descarga y parsea una playlist M3U, reemplazando canales.
// Variante síncrona: adquiere el lock, ejecuta el import, libera.
func (s *Service) RefreshM3U(ctx context.Context, libraryID string) (int, error) {
	release, err := s.TryAcquireRefresh(libraryID)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.RunRefreshM3U(ctx, libraryID)
}

// PublishRefreshFailed emite un evento SSE playlist.refresh_failed para
// que la UI admin pueda limpiar el spinner.
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

// RunRefreshM3U ejecuta el import real. El caller es responsable del
// lock per-library vía TryAcquireRefresh.
func (s *Service) RunRefreshM3U(ctx context.Context, libraryID string) (int, error) {
	lib, err := s.libraries.GetByID(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("get library: %w", err)
	}
	if lib.M3UURL == "" {
		return 0, fmt.Errorf("library %s has no M3U URL configured", libraryID)
	}

	s.logger.Info("refreshing M3U playlist", "library", libraryID, "url", lib.M3UURL)

	// 1. Fetch + parse del M3U stream.
	dbChannels, playlistEPGURL, err := s.fetchAndParseM3U(ctx, libraryID, lib)
	if err != nil {
		return 0, err
	}

	// 2. Persistir canales en DB.
	if err := s.persistChannels(ctx, libraryID, dbChannels); err != nil {
		return 0, err
	}

	// 3. Re-aplicar overrides del admin.
	s.reapplyOverrides(ctx, libraryID)

	// 4. Persistir EPG URL descubierta y publicar evento.
	epgDiscovered := s.persistDiscoveredEPG(ctx, libraryID, playlistEPGURL, lib)

	s.logger.Info("M3U refresh complete", "library", libraryID, "channels", len(dbChannels))
	s.publish(event.Event{
		Type: event.PlaylistRefreshed,
		Data: map[string]any{
			"library_id":     libraryID,
			"channels_count": len(dbChannels),
		},
	})

	// 5. Disparar tareas background (EPG + probe).
	s.triggerPostImportTasks(libraryID, epgDiscovered)

	return len(dbChannels), nil
}

// fetchAndParseM3U descarga y parsea el M3U stream, aplicando filtros
// VOD y de idioma. Devuelve canales listos para persistir y la URL
// EPG descubierta (si existe).
func (s *Service) fetchAndParseM3U(ctx context.Context, libraryID string, lib *librarymodel.Library) ([]*iptvmodel.Channel, string, error) {
	body, err := s.fetchURL(ctx, lib.M3UURL, lib.TLSInsecure)
	if err != nil {
		return nil, "", fmt.Errorf("fetch M3U: %w", err)
	}
	defer body.Close() //nolint:errcheck

	now := time.Now()
	var (
		dbChannels      []*iptvmodel.Channel
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

	// Tolerar descargas truncadas: si ya hay suficientes canales, commitear.
	if parseErr != nil {
		if errors.Is(parseErr, ErrNotM3U) {
			s.logger.Error("M3U source did not return a playlist",
				"library", libraryID, "url", lib.M3UURL, "hint", "HTML/error page received")
			return nil, "", fmt.Errorf("the M3U URL returned an HTML page, not a playlist — "+
				"likely causes: account suspended, IP blocked, bad credentials, or rate-limit. "+
				"Verify the URL in a browser. Underlying: %w", parseErr)
		}
		const minUsable = 50
		if len(dbChannels) >= minUsable {
			s.logger.Warn("M3U parse truncated; importing what we got",
				"library", libraryID, "lines", parsedLines,
				"channels", len(dbChannels), "vod_skipped", vodSkipped,
				"language_skipped", languageSkipped, "error", parseErr)
		} else {
			return nil, "", fmt.Errorf("parse M3U: %w", parseErr)
		}
	} else {
		s.logger.Info("M3U parse complete",
			"library", libraryID, "lines", parsedLines,
			"channels", len(dbChannels), "vod_skipped", vodSkipped,
			"language_skipped", languageSkipped)
	}

	return dbChannels, playlistEPGURL, nil
}

// persistChannels reemplaza los canales de la biblioteca en la DB.
func (s *Service) persistChannels(ctx context.Context, libraryID string, dbChannels []*iptvmodel.Channel) error {
	if err := s.channels.ReplaceForLibrary(ctx, libraryID, dbChannels); err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}
	return nil
}

// reapplyOverrides re-aplica campos editados a mano (tvg_id) desde la
// tabla de overrides. Los overrides huérfanos son no-op.
func (s *Service) reapplyOverrides(ctx context.Context, libraryID string) {
	if s.overrides == nil {
		return
	}
	if n, err := s.overrides.ApplyToLibrary(ctx, libraryID); err != nil {
		s.logger.Error("apply channel overrides post-import",
			"library", libraryID, "error", err)
	} else if n > 0 {
		s.logger.Info("reapplied channel overrides",
			"library", libraryID, "count", n)
	}
}

// persistDiscoveredEPG guarda la URL XMLTV que la playlist anunció,
// solo si la biblioteca no tiene una configurada. Devuelve true si
// se descubrió y persistió una URL nueva.
func (s *Service) persistDiscoveredEPG(ctx context.Context, libraryID, playlistEPGURL string, lib *librarymodel.Library) bool {
	if playlistEPGURL == "" || lib.EPGURL != "" {
		return false
	}
	now := time.Now()
	lib.EPGURL = playlistEPGURL
	lib.UpdatedAt = now
	if err := s.libraries.Update(ctx, lib); err != nil {
		s.logger.Warn("persist discovered EPG URL",
			"library", libraryID, "epg_url", playlistEPGURL, "error", err)
		return false
	}
	s.logger.Info("discovered EPG URL from playlist header",
		"library", libraryID, "epg_url", playlistEPGURL)
	return true
}

// triggerPostImportTasks dispara EPG refresh y probe activo en
// background. Usa SpawnBackground para que Shutdown drene la goroutine.
func (s *Service) triggerPostImportTasks(libraryID string, epgDiscovered bool) {
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

// parseLanguageFilter separa el valor de columna comma-separated en la
// slice que MatchesLanguageFilter espera.
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
