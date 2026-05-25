package iptv

// ChannelOrderOps agrupa orden/visibilidad/logo de canales:
//  1. Overlay per-user (user_channel_order)
//  2. Overlay admin de library (library_channel_order)
//  3. Overrides de logo (channel_logo_overrides)
//
// Los tres comparten overlay-at-read-time sobre la lista de canales.
// iptvOrgLogos es nil-safe: sin él /iptv-org/refresh-logos devuelve 503.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
)

// LocalLogoSentinel — prefijo que indica logo subido a disco.
// El proxy de logos detecta este prefijo y sirve desde
// <imageDir>/channel-logos/ en vez del cache remoto.
const LocalLogoSentinel = "hubplay-local:channel-logos/"

// IPTVOrgRefreshSummary — desglose de una corrida de RefreshLogosFromIPTVOrg
// para que la UI explique por qué se actualizaron 0 logos.
type IPTVOrgRefreshSummary struct {
	Total              int `json:"total"`
	AlreadyHaveLogo    int `json:"already_have_logo"`
	WithoutTvgID       int `json:"without_tvg_id"`
	SkippedHasOverride int `json:"skipped_has_override"`
	NotInDatabase      int `json:"not_in_database"`
	Updated            int `json:"updated"`
}

// ChannelOrderOps — stateless por construcción: el overlay se aplica
// en cada lectura sin estado mutable propio.
type ChannelOrderOps struct {
	channels            *db.ChannelRepository
	channelOrder        *db.UserChannelOrderRepository
	libraryChannelOrder *db.LibraryChannelOrderRepository
	logoOverrides       *db.ChannelLogoOverrideRepository

	// iptvOrgLogos — nil-safe: sin él el endpoint devuelve 503.
	iptvOrgLogos *IPTVOrgLogoLookup

	logger *slog.Logger
}

func newChannelOrderOps(
	channels *db.ChannelRepository,
	channelOrder *db.UserChannelOrderRepository,
	libraryChannelOrder *db.LibraryChannelOrderRepository,
	logoOverrides *db.ChannelLogoOverrideRepository,
	logger *slog.Logger,
) *ChannelOrderOps {
	return &ChannelOrderOps{
		channels:            channels,
		channelOrder:        channelOrder,
		libraryChannelOrder: libraryChannelOrder,
		logoOverrides:       logoOverrides,
		logger:              logger,
	}
}

// SetIPTVOrgLogos cablea el lookup iptv-org post-construcción.
func (c *ChannelOrderOps) SetIPTVOrgLogos(l *IPTVOrgLogoLookup) { c.iptvOrgLogos = l }

// listChannels replica GetChannels sin depender del Service (el
// receiver es *ChannelOrderOps, no puede llamar a la facade).
func (c *ChannelOrderOps) listChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*iptvmodel.Channel, error) {
	if activeOnly {
		return c.channels.ListHealthyByLibrary(ctx, libraryID)
	}
	return c.channels.ListByLibrary(ctx, libraryID, false)
}

// ── Helpers de overlay puro ──────────────────────────────────────
//
// El overlay se aplica en lectura, no en escritura: no hay snapshot
// per-user. Un usuario sin overrides ve el orden del admin tal cual.

// applyLogoOverlay sustituye LogoURL con el override admin cuando existe.
// Puro: devuelve un slice nuevo, O(N+M). Indexado por stream_url para
// sobrevivir re-imports (los UUIDs se regeneran en cada refresh).
func applyLogoOverlay(channels []*iptvmodel.Channel, overrides []iptvmodel.ChannelLogoOverride) []*iptvmodel.Channel {
	if len(overrides) == 0 {
		return channels
	}
	byURL := make(map[string]iptvmodel.ChannelLogoOverride, len(overrides))
	for _, o := range overrides {
		byURL[o.StreamURL] = o
	}
	out := make([]*iptvmodel.Channel, 0, len(channels))
	for _, c := range channels {
		o, has := byURL[c.StreamURL]
		if !has {
			out = append(out, c)
			continue
		}
		cp := *c
		switch {
		case o.LogoFile != "":
			cp.LogoURL = LocalLogoSentinel + o.LogoFile
		case o.LogoURL != "":
			cp.LogoURL = o.LogoURL
		}
		out = append(out, &cp)
	}
	return out
}

// applyAdminOverlay aplica la curación admin sobre el import M3U.
// Canales con hidden=true se eliminan (restricción dura: los usuarios
// no pueden des-ocultar). Puro, O(N+M), ordena por posición.
func applyAdminOverlay(channels []*iptvmodel.Channel, overrides []iptvmodel.LibraryChannelOrderEntry) []*iptvmodel.Channel {
	if len(overrides) == 0 {
		return channels
	}
	byID := make(map[string]iptvmodel.LibraryChannelOrderEntry, len(overrides))
	for _, o := range overrides {
		byID[o.ChannelID] = o
	}

	out := make([]*iptvmodel.Channel, 0, len(channels))
	for _, c := range channels {
		o, has := byID[c.ID]
		if has && o.Hidden {
			continue
		}
		cp := *c
		if has {
			cp.Number = o.Position
		}
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Number < out[j].Number
	})
	return out
}

// applyOrderOverlay aplica el overlay per-user. Los canales hidden
// se eliminan. Puro, O(N+M), ordena por posición efectiva.
func applyOrderOverlay(channels []*iptvmodel.Channel, overrides []iptvmodel.UserChannelOrderEntry) []*iptvmodel.Channel {
	if len(overrides) == 0 {
		return channels
	}
	byID := make(map[string]iptvmodel.UserChannelOrderEntry, len(overrides))
	for _, o := range overrides {
		byID[o.ChannelID] = o
	}

	out := make([]*iptvmodel.Channel, 0, len(channels))
	for _, c := range channels {
		o, has := byID[c.ID]
		if has && o.Hidden {
			continue
		}
		// Clonamos para no mutar el slice cacheado del repo.
		cp := *c
		if has {
			cp.Number = o.Position
		}
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Stable: empates conservan el orden original del M3U.
		return out[i].Number < out[j].Number
	})
	return out
}

// ── Lecturas con cascada de overlay ──────────────────────────────

// GetChannelsForUser devuelve la lista de canales para un usuario:
// logo overlay → admin overlay → user overlay. userID vacío = vista admin.
func (c *ChannelOrderOps) GetChannelsForUser(ctx context.Context, libraryID, userID string, activeOnly bool) ([]*iptvmodel.Channel, error) {
	channels, err := c.listChannels(ctx, libraryID, activeOnly)
	if err != nil {
		return nil, err
	}

	// Logo overlay primero para que todas las capas posteriores
	// vean el LogoURL final.
	if c.logoOverrides != nil {
		logoRows, err := c.logoOverrides.ListByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("load channel logo overrides: %w", err)
		}
		channels = applyLogoOverlay(channels, logoRows)
	}

	// Admin overlay — restricción dura: el usuario no puede des-ocultar.
	if c.libraryChannelOrder != nil {
		adminRows, err := c.libraryChannelOrder.List(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("load library channel order: %w", err)
		}
		channels = applyAdminOverlay(channels, adminRows)
	}

	if userID == "" || c.channelOrder == nil {
		return channels, nil
	}
	overrides, err := c.channelOrder.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user channel order: %w", err)
	}
	return applyOrderOverlay(channels, overrides), nil
}

// GetChannelsForUserPersonalisation devuelve TODOS los canales
// (incluso user-hidden) para el panel /live-tv/customize. Los
// admin-hidden sí se filtran (restricción dura).
func (c *ChannelOrderOps) GetChannelsForUserPersonalisation(ctx context.Context, libraryID, userID string) ([]*iptvmodel.Channel, error) {
	channels, err := c.listChannels(ctx, libraryID, false)
	if err != nil {
		return nil, err
	}
	if c.logoOverrides != nil {
		logoRows, lErr := c.logoOverrides.ListByLibrary(ctx, libraryID)
		if lErr != nil {
			return nil, fmt.Errorf("load channel logo overrides: %w", lErr)
		}
		channels = applyLogoOverlay(channels, logoRows)
	}
	// Admin overlay — restricción dura.
	if c.libraryChannelOrder != nil {
		adminRows, aErr := c.libraryChannelOrder.List(ctx, libraryID)
		if aErr != nil {
			return nil, fmt.Errorf("load library channel order: %w", aErr)
		}
		channels = applyAdminOverlay(channels, adminRows)
	}
	if userID == "" || c.channelOrder == nil {
		return channels, nil
	}
	overrides, err := c.channelOrder.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user channel order: %w", err)
	}
	// Aplica posición del usuario SIN filtrar hidden (el panel los muestra).
	byID := make(map[string]iptvmodel.UserChannelOrderEntry, len(overrides))
	for _, o := range overrides {
		byID[o.ChannelID] = o
	}
	out := make([]*iptvmodel.Channel, 0, len(channels))
	for _, ch := range channels {
		cp := *ch
		if o, has := byID[ch.ID]; has {
			cp.Number = o.Position
		}
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// GetChannelsForLibraryAdmin devuelve la vista de curación admin.
// includeHidden=true incluye los admin-hidden para el toggle del panel.
func (c *ChannelOrderOps) GetChannelsForLibraryAdmin(ctx context.Context, libraryID string, includeHidden bool) ([]*iptvmodel.Channel, []iptvmodel.LibraryChannelOrderEntry, error) {
	channels, err := c.listChannels(ctx, libraryID, false)
	if err != nil {
		return nil, nil, err
	}
	// Logo overlay para que el admin vea el logo efectivo.
	if c.logoOverrides != nil {
		logoRows, lErr := c.logoOverrides.ListByLibrary(ctx, libraryID)
		if lErr != nil {
			return nil, nil, fmt.Errorf("load channel logo overrides: %w", lErr)
		}
		channels = applyLogoOverlay(channels, logoRows)
	}
	var rows []iptvmodel.LibraryChannelOrderEntry
	if c.libraryChannelOrder != nil {
		rows, err = c.libraryChannelOrder.List(ctx, libraryID)
		if err != nil {
			return nil, nil, fmt.Errorf("load library channel order: %w", err)
		}
	}
	if includeHidden {
		// No reusamos applyAdminOverlay porque filtra hidden;
		// aquí los mantenemos para el toggle de visibilidad.
		byID := make(map[string]iptvmodel.LibraryChannelOrderEntry, len(rows))
		for _, o := range rows {
			byID[o.ChannelID] = o
		}
		out := make([]*iptvmodel.Channel, 0, len(channels))
		for _, ch := range channels {
			cp := *ch
			if o, has := byID[ch.ID]; has {
				cp.Number = o.Position
			}
			out = append(out, &cp)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
		return out, rows, nil
	}
	return applyAdminOverlay(channels, rows), rows, nil
}

// ── Escrituras de curación admin ─────────────────────────────────

// ListLibraryChannelOverrides devuelve los overrides admin de una library.
func (c *ChannelOrderOps) ListLibraryChannelOverrides(ctx context.Context, libraryID string) ([]iptvmodel.LibraryChannelOrderEntry, error) {
	if c.libraryChannelOrder == nil {
		return nil, nil
	}
	return c.libraryChannelOrder.List(ctx, libraryID)
}

// ReplaceLibraryChannelOrder persiste el orden completo del admin.
// Los IDs ausentes pierden su override y vuelven al orden del M3U.
func (c *ChannelOrderOps) ReplaceLibraryChannelOrder(ctx context.Context, libraryID string, orderedIDs []string, hiddenIDs map[string]bool) error {
	if c.libraryChannelOrder == nil {
		return fmt.Errorf("library channel order repo not wired")
	}
	entries := make([]iptvmodel.LibraryChannelOrderEntry, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entries = append(entries, iptvmodel.LibraryChannelOrderEntry{
			ChannelID: id,
			Hidden:    hiddenIDs[id],
		})
	}
	return c.libraryChannelOrder.ReplaceAll(ctx, libraryID, entries)
}

// SetLibraryChannelVisibility cambia el hidden de un canal a nivel admin.
// Evita re-subir la lista completa cuando solo se oculta uno.
func (c *ChannelOrderOps) SetLibraryChannelVisibility(ctx context.Context, libraryID, channelID string, hidden bool) error {
	if c.libraryChannelOrder == nil {
		return fmt.Errorf("library channel order repo not wired")
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	if ch.LibraryID != libraryID {
		return fmt.Errorf("channel %s does not belong to library %s", channelID, libraryID)
	}
	rows, err := c.libraryChannelOrder.List(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("list library channel order: %w", err)
	}
	position := ch.Number
	for _, o := range rows {
		if o.ChannelID == channelID {
			position = o.Position
			break
		}
	}
	return c.libraryChannelOrder.Upsert(ctx, libraryID, channelID, position, hidden)
}

// ResetLibraryChannelOrder borra todos los overrides admin de una library.
func (c *ChannelOrderOps) ResetLibraryChannelOrder(ctx context.Context, libraryID string) error {
	if c.libraryChannelOrder == nil {
		return nil
	}
	return c.libraryChannelOrder.Reset(ctx, libraryID)
}

// ── Escrituras per-user ──────────────────────────────────────────

// ListChannelOverrides devuelve las filas de override del usuario.
func (c *ChannelOrderOps) ListChannelOverrides(ctx context.Context, userID string) ([]iptvmodel.UserChannelOrderEntry, error) {
	if c.channelOrder == nil {
		return nil, nil
	}
	return c.channelOrder.List(ctx, userID)
}

// ReplaceChannelOrder persiste el orden per-user completo.
// Los IDs ausentes pierden su override y vuelven a defaults admin.
func (c *ChannelOrderOps) ReplaceChannelOrder(ctx context.Context, userID string, orderedIDs []string, hiddenIDs map[string]bool) error {
	if c.channelOrder == nil {
		return fmt.Errorf("channel order repo not wired")
	}
	entries := make([]iptvmodel.UserChannelOrderEntry, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entries = append(entries, iptvmodel.UserChannelOrderEntry{
			ChannelID: id,
			Hidden:    hiddenIDs[id],
		})
	}
	return c.channelOrder.ReplaceAll(ctx, userID, entries)
}

// SetChannelVisibility cambia el hidden de un canal para el usuario.
// Si no existe override, inserta con position = Number actual para
// no alterar el orden visible.
func (c *ChannelOrderOps) SetChannelVisibility(ctx context.Context, userID, channelID string, hidden bool) error {
	if c.channelOrder == nil {
		return fmt.Errorf("channel order repo not wired")
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	overrides, err := c.channelOrder.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("list overrides: %w", err)
	}
	position := ch.Number
	for _, o := range overrides {
		if o.ChannelID == channelID {
			position = o.Position
			break
		}
	}
	return c.channelOrder.Upsert(ctx, userID, channelID, position, hidden)
}

// ResetChannelOrder borra todos los overrides del usuario.
func (c *ChannelOrderOps) ResetChannelOrder(ctx context.Context, userID string) error {
	if c.channelOrder == nil {
		return nil
	}
	return c.channelOrder.Reset(ctx, userID)
}

// ── Overrides de logo de canal ────────────────────────────────────
//
// Indexados por (library_id, stream_url) para sobrevivir re-imports M3U.

// SetChannelLogoURL escribe un override de URL externa para el canal.
func (c *ChannelOrderOps) SetChannelLogoURL(ctx context.Context, channelID, logoURL string) error {
	if c.logoOverrides == nil {
		return fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	if logoURL == "" {
		return fmt.Errorf("iptv: logo_url required (use ClearChannelLogo to remove)")
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return err
	}
	return c.logoOverrides.UpsertURL(ctx, ch.LibraryID, ch.StreamURL, logoURL)
}

// SetChannelLogoFile guarda un override de archivo subido.
// Devuelve el basename previo para que el handler borre el viejo.
func (c *ChannelOrderOps) SetChannelLogoFile(ctx context.Context, channelID, basename string) (previousFile string, err error) {
	if c.logoOverrides == nil {
		return "", fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	if basename == "" {
		return "", fmt.Errorf("iptv: file basename required")
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return "", err
	}
	prev, err := c.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
	if err != nil {
		return "", err
	}
	previousFile = ""
	if prev != nil {
		previousFile = prev.LogoFile
	}
	if err := c.logoOverrides.UpsertFile(ctx, ch.LibraryID, ch.StreamURL, basename); err != nil {
		return "", err
	}
	return previousFile, nil
}

// ClearChannelLogo borra el override del canal. Devuelve el basename
// previo para que el handler borre el archivo huérfano.
func (c *ChannelOrderOps) ClearChannelLogo(ctx context.Context, channelID string) (previousFile string, err error) {
	if c.logoOverrides == nil {
		return "", fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return "", err
	}
	prev, err := c.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
	if err != nil {
		return "", err
	}
	previousFile = ""
	if prev != nil {
		previousFile = prev.LogoFile
	}
	if err := c.logoOverrides.Delete(ctx, ch.LibraryID, ch.StreamURL); err != nil {
		return "", err
	}
	return previousFile, nil
}

// GetChannelLogoOverride devuelve el override actual (URL, archivo, o nil).
func (c *ChannelOrderOps) GetChannelLogoOverride(ctx context.Context, channelID string) (*iptvmodel.ChannelLogoOverride, error) {
	if c.logoOverrides == nil {
		return nil, nil
	}
	ch, err := c.channels.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return c.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
}

// RefreshLogosFromIPTVOrg busca logos en la base pública de iptv-org
// (mapeo por tvg_id) para cada canal de la biblioteca que:
//   - No tenga ya un override admin (URL o archivo).
//   - No traiga tvg-logo del M3U.
//   - Tenga un tvg_id no vacío que se pueda usar como clave de búsqueda.
//
// Los hallazgos se guardan como overrides URL (no se tocan los datos
// originales del M3U). El admin puede borrarlos uno a uno desde el
// modal de logo del canal — "Restaurar logo del M3U" deja el override
// en blanco y vuelve al estado anterior.
//
// Devuelve un summary con desglose para que la UI explique qué pasó:
// "47 canales actualizados", o "0 actualizados porque tus 120 canales
// ya tienen logo del M3U", o "0 actualizados porque tus tvg-ids no
// coinciden con los de iptv-org".
func (c *ChannelOrderOps) RefreshLogosFromIPTVOrg(ctx context.Context, libraryID string) (IPTVOrgRefreshSummary, error) {
	var sum IPTVOrgRefreshSummary
	if c.iptvOrgLogos == nil {
		return sum, fmt.Errorf("iptv: iptv-org lookup not configured")
	}
	if c.logoOverrides == nil {
		return sum, fmt.Errorf("iptv: logo overrides repository not wired")
	}

	lookup, err := c.iptvOrgLogos.Load(ctx)
	if err != nil {
		return sum, fmt.Errorf("load iptv-org lookup: %w", err)
	}

	channels, err := c.listChannels(ctx, libraryID, false)
	if err != nil {
		return sum, err
	}
	sum.Total = len(channels)

	// Bulk-load overrides previos para no tener que consultarlos uno
	// a uno (sería N+1 contra DB para libraries grandes).
	existing, err := c.logoOverrides.ListByLibrary(ctx, libraryID)
	if err != nil {
		return sum, fmt.Errorf("load existing logo overrides: %w", err)
	}
	hasOverride := make(map[string]bool, len(existing))
	for _, o := range existing {
		hasOverride[o.StreamURL] = true
	}

	for _, ch := range channels {
		if ch.LogoURL != "" {
			sum.AlreadyHaveLogo++
			continue
		}
		if ch.TvgID == "" {
			sum.WithoutTvgID++
			continue
		}
		if hasOverride[ch.StreamURL] {
			sum.SkippedHasOverride++
			continue
		}
		logo, ok := lookup[strings.ToLower(ch.TvgID)]
		if !ok || logo == "" {
			sum.NotInDatabase++
			continue
		}
		if err := c.logoOverrides.UpsertURL(ctx, libraryID, ch.StreamURL, logo); err != nil {
			c.logger.Warn("iptv-org logo upsert failed",
				"library", libraryID, "tvg_id", ch.TvgID, "error", err)
			continue
		}
		sum.Updated++
	}
	return sum, nil
}
