package iptv

// Per-user channel order + visibility overlay. The admin uploads
// M3U lists and the resulting Channel.Number is the initial order
// every viewer sees; this file lets each user override that for
// their own account.
//
// The overlay is applied at read time, not at write time: there is
// no per-user snapshot of the channel list. A user with no override
// rows sees the admin's order verbatim — moving one channel writes
// one row, everything else still inherits from the admin defaults.

import (
	"context"
	"fmt"
	"sort"

	iptvmodel "hubplay/internal/iptv/model"
)

// applyLogoOverlay swaps Channel.LogoURL with the admin's custom logo
// when there's a matching row in `channel_logo_overrides`. Two cases:
//
//   - logo_file set → the local-file route: emit a sentinel URL that
//     the frontend keeps untouched (the channel-logo proxy resolves
//     it to disk on GET). Format: "hubplay-local:channel-logos/<file>".
//   - logo_url set  → the external-URL route: replace LogoURL with the
//     admin's URL, which the existing logo cache will fetch on demand
//     just like any other upstream image.
//
// Pure: the input slice is not mutated; a fresh slice is returned.
// O(N + M) where N = channels, M = overrides. Channels without a
// matching override row are passed through with their M3U LogoURL.
//
// Indexed by (library_id, stream_url) — same key the override table
// uses — so the M3U refresh (which regenerates channel UUIDs) doesn't
// orphan overrides on the next import. Two channels sharing a stream
// URL inside the same library would also share the override; in
// practice the M3U importer dedupes by stream URL so this is moot.
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

// LocalLogoSentinel is the prefix the logo overlay tags onto
// Channel.LogoURL when the admin uploaded a file. The channel-logo
// proxy detects this prefix and serves the file directly from
// <imageDir>/channel-logos/ instead of going through the remote cache.
// Exported so the handler that serves /channels/{id}/logo can switch
// on it without re-deriving the convention.
const LocalLogoSentinel = "hubplay-local:channel-logos/"

// applyAdminOverlay applies the library's admin curation
// (`library_channel_order`) on top of the raw M3U import. Channels
// with a matching override row take the admin's position; rows
// flagged hidden are stripped (hard constraint — users cannot
// un-hide what the admin removed).
//
// Channels without an override row keep their M3U-import number.
// Result is sorted ascending by effective number.
//
// Pure: the input slice is not mutated; a fresh slice is returned.
// O(N + M) where N = channels, M = overrides.
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

// applyOrderOverlay overlays a user's `user_channel_order` rows onto
// a channel list returned by `GetChannels`. Channels with a
// matching override row use the user's position and hidden flag;
// the rest fall through to `Channel.Number` (admin default).
//
// Hidden channels are stripped from the slice. The result is
// sorted ascending by effective position.
//
// Pure: the input slice is not mutated; a fresh slice is returned.
// O(N + M) where N = channels, M = overrides.
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
		// We clone the channel so a future caller mutating the
		// returned slice can't accidentally clobber the cached
		// version the repo returned. Number takes the override
		// when present so downstream consumers (sorts, group
		// renderers) see the user's position.
		cp := *c
		if has {
			cp.Number = o.Position
		}
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Stable sort on Number — ties (channels with the same
		// admin Number) keep their original order so the M3U
		// import sequence stays meaningful as a tiebreaker.
		return out[i].Number < out[j].Number
	})
	return out
}

// GetChannelsForUser returns the channel list the given user should
// see: admin defaults overlaid with the user's per-channel
// overrides. activeOnly behaves the same as GetChannels (filters
// out unhealthy channels at the DB layer); hidden-by-user channels
// are filtered in the overlay step.
//
// When userID is empty (no authenticated user, admin contexts) the
// overlay step is skipped and this returns the admin view.
func (s *Service) GetChannelsForUser(ctx context.Context, libraryID, userID string, activeOnly bool) ([]*iptvmodel.Channel, error) {
	channels, err := s.GetChannels(ctx, libraryID, activeOnly)
	if err != nil {
		return nil, err
	}

	// Logo overlay primero — antes que cualquier filtro de orden /
	// visibilidad para que el LogoURL final lo vean todas las capas
	// (incluido el continue-watching que clona Channel sin volver a
	// pasar por aquí).
	if s.logoOverrides != nil {
		logoRows, err := s.logoOverrides.ListByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("load channel logo overrides: %w", err)
		}
		channels = applyLogoOverlay(channels, logoRows)
	}

	// Admin overlay — applies the library's curated order and removes
	// admin-hidden channels (hard constraint, the user cannot surface
	// them again via their own overlay).
	if s.libraryChannelOrder != nil {
		adminRows, err := s.libraryChannelOrder.List(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("load library channel order: %w", err)
		}
		channels = applyAdminOverlay(channels, adminRows)
	}

	if userID == "" || s.channelOrder == nil {
		return channels, nil
	}
	overrides, err := s.channelOrder.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user channel order: %w", err)
	}
	return applyOrderOverlay(channels, overrides), nil
}

// GetChannelsForLibraryAdmin returns the curation view used by the
// admin panel at /admin/libraries/{id}: raw channel rows ordered by
// the admin overlay (so the operator sees the current default), but
// `includeHidden=true` skips the admin-hidden filter so the panel
// can render an editable list with a visibility toggle per row.
//
// `includeHidden=false` is equivalent to "what every non-admin user
// sees before their own overlay" — useful for previews.
func (s *Service) GetChannelsForLibraryAdmin(ctx context.Context, libraryID string, includeHidden bool) ([]*iptvmodel.Channel, []iptvmodel.LibraryChannelOrderEntry, error) {
	channels, err := s.GetChannels(ctx, libraryID, false)
	if err != nil {
		return nil, nil, err
	}
	// Logo overlay también para el admin: el panel de curación muestra
	// el logo efectivo (con override aplicado) para que el operador vea
	// el estado real, no el M3U bruto.
	if s.logoOverrides != nil {
		logoRows, lErr := s.logoOverrides.ListByLibrary(ctx, libraryID)
		if lErr != nil {
			return nil, nil, fmt.Errorf("load channel logo overrides: %w", lErr)
		}
		channels = applyLogoOverlay(channels, logoRows)
	}
	var rows []iptvmodel.LibraryChannelOrderEntry
	if s.libraryChannelOrder != nil {
		rows, err = s.libraryChannelOrder.List(ctx, libraryID)
		if err != nil {
			return nil, nil, fmt.Errorf("load library channel order: %w", err)
		}
	}
	if includeHidden {
		// Apply position from overlay but keep hidden rows in the
		// list so the panel can render the eye-off toggle next to
		// them. We can't reuse applyAdminOverlay (it filters
		// hidden); inline the position merge.
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

// ListLibraryChannelOverrides returns the admin override rows for a
// library. Used by the curation panel to compute which channels
// have been touched vs. which still inherit the M3U order.
func (s *Service) ListLibraryChannelOverrides(ctx context.Context, libraryID string) ([]iptvmodel.LibraryChannelOrderEntry, error) {
	if s.libraryChannelOrder == nil {
		return nil, nil
	}
	return s.libraryChannelOrder.List(ctx, libraryID)
}

// ReplaceLibraryChannelOrder is the admin panel's "Save order"
// entry point: it receives the full reordered list of channel IDs
// and persists position = index+1 for each, in a single
// transaction. `hiddenIDs` is the set of channels the admin marked
// hidden — applied as a hard constraint downstream of the user
// overlay.
//
// Channels NOT present in `orderedIDs` lose their override row and
// fall back to channels.number from the M3U import.
func (s *Service) ReplaceLibraryChannelOrder(ctx context.Context, libraryID string, orderedIDs []string, hiddenIDs map[string]bool) error {
	if s.libraryChannelOrder == nil {
		return fmt.Errorf("library channel order repo not wired")
	}
	entries := make([]iptvmodel.LibraryChannelOrderEntry, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entries = append(entries, iptvmodel.LibraryChannelOrderEntry{
			ChannelID: id,
			Hidden:    hiddenIDs[id],
		})
	}
	return s.libraryChannelOrder.ReplaceAll(ctx, libraryID, entries)
}

// SetLibraryChannelVisibility flips a single channel's hidden state
// at the admin level (hard constraint). Same surgical-edit pattern
// as the per-user counterpart: avoids re-uploading the full
// reordered list when the admin just wants to hide one channel.
func (s *Service) SetLibraryChannelVisibility(ctx context.Context, libraryID, channelID string, hidden bool) error {
	if s.libraryChannelOrder == nil {
		return fmt.Errorf("library channel order repo not wired")
	}
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	if ch.LibraryID != libraryID {
		return fmt.Errorf("channel %s does not belong to library %s", channelID, libraryID)
	}
	rows, err := s.libraryChannelOrder.List(ctx, libraryID)
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
	return s.libraryChannelOrder.Upsert(ctx, libraryID, channelID, position, hidden)
}

// ResetLibraryChannelOrder wipes every admin override for a library
// — channels fall back to channels.number from the M3U import.
func (s *Service) ResetLibraryChannelOrder(ctx context.Context, libraryID string) error {
	if s.libraryChannelOrder == nil {
		return nil
	}
	return s.libraryChannelOrder.Reset(ctx, libraryID)
}

// ListChannelOverrides returns the user's raw override rows for the
// personalisation panel. The panel renders these alongside the
// channel list so the user can see which channels they've touched
// (highlighted) vs. which still inherit the admin defaults.
func (s *Service) ListChannelOverrides(ctx context.Context, userID string) ([]iptvmodel.UserChannelOrderEntry, error) {
	if s.channelOrder == nil {
		return nil, nil
	}
	return s.channelOrder.List(ctx, userID)
}

// ReplaceChannelOrder is the panel's "Save order" entry point: it
// receives the full reordered list of channel IDs and persists
// position = index+1 for each, in a single transaction.
//
// The hidden flag is preserved for IDs the caller marked as
// hidden via `hiddenIDs` (set semantics — pass the same channelID
// once even if it's also in `orderedIDs`). Channels not present
// in `orderedIDs` lose their override row and fall back to admin
// defaults — that's how "opt out for a subset" works.
func (s *Service) ReplaceChannelOrder(ctx context.Context, userID string, orderedIDs []string, hiddenIDs map[string]bool) error {
	if s.channelOrder == nil {
		return fmt.Errorf("channel order repo not wired")
	}
	entries := make([]iptvmodel.UserChannelOrderEntry, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entries = append(entries, iptvmodel.UserChannelOrderEntry{
			ChannelID: id,
			Hidden:    hiddenIDs[id],
		})
	}
	return s.channelOrder.ReplaceAll(ctx, userID, entries)
}

// SetChannelVisibility flips a single channel's hidden state for
// the given user. Touching only one row avoids the "save the whole
// list" round trip when the user just wants to hide one channel
// from the channel list view.
//
// Implementation: when the user hides a channel that doesn't have
// an override yet, we insert with position = current admin Number
// so the visible order is unchanged. When they un-hide an existing
// override, we keep their position and just flip the flag.
func (s *Service) SetChannelVisibility(ctx context.Context, userID, channelID string, hidden bool) error {
	if s.channelOrder == nil {
		return fmt.Errorf("channel order repo not wired")
	}
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	overrides, err := s.channelOrder.List(ctx, userID)
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
	return s.channelOrder.Upsert(ctx, userID, channelID, position, hidden)
}

// ResetChannelOrder wipes every override the user has, restoring
// the admin's default order and visibility. The personalisation
// panel's "Restore admin order" button calls this.
func (s *Service) ResetChannelOrder(ctx context.Context, userID string) error {
	if s.channelOrder == nil {
		return nil
	}
	return s.channelOrder.Reset(ctx, userID)
}

// ── Channel logo overrides ─────────────────────────────────────────
//
// Admin-only flow para reemplazar el logo de un canal. La row vive en
// channel_logo_overrides indexada por (library_id, stream_url) — misma
// invariante que ChannelOverride — para sobrevivir al re-import del M3U
// (los UUIDs de canales se regeneran en cada refresh).

// SetChannelLogoURL escribe (o reemplaza) un override de URL externa
// para el canal. El stream_url se resuelve desde la row de channels en
// el momento de la escritura — si el M3U se ha refrescado entre dos
// llamadas el nuevo stream_url cuenta a partir de ese instante.
func (s *Service) SetChannelLogoURL(ctx context.Context, channelID, logoURL string) error {
	if s.logoOverrides == nil {
		return fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	if logoURL == "" {
		return fmt.Errorf("iptv: logo_url required (use ClearChannelLogo to remove)")
	}
	ch, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}
	return s.logoOverrides.UpsertURL(ctx, ch.LibraryID, ch.StreamURL, logoURL)
}

// SetChannelLogoFile guarda un override de archivo subido para el
// canal. Devuelve el basename del archivo PREVIO (si lo había), así el
// handler que orquesta la subida puede borrar el archivo viejo del
// disco sin tener que hacer un Get aparte. Devuelve "" cuando no había
// override previo o el previo era una URL.
func (s *Service) SetChannelLogoFile(ctx context.Context, channelID, basename string) (previousFile string, err error) {
	if s.logoOverrides == nil {
		return "", fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	if basename == "" {
		return "", fmt.Errorf("iptv: file basename required")
	}
	ch, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return "", err
	}
	prev, err := s.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
	if err != nil {
		return "", err
	}
	previousFile = ""
	if prev != nil {
		previousFile = prev.LogoFile
	}
	if err := s.logoOverrides.UpsertFile(ctx, ch.LibraryID, ch.StreamURL, basename); err != nil {
		return "", err
	}
	return previousFile, nil
}

// ClearChannelLogo borra el override (URL o file) del canal — el
// listado vuelve a usar el tvg-logo del M3U a partir del siguiente
// fetch. Devuelve el basename del archivo previo (si lo había) para
// que el handler borre el archivo huérfano del disco.
func (s *Service) ClearChannelLogo(ctx context.Context, channelID string) (previousFile string, err error) {
	if s.logoOverrides == nil {
		return "", fmt.Errorf("iptv: channel logo overrides repository not wired")
	}
	ch, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return "", err
	}
	prev, err := s.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
	if err != nil {
		return "", err
	}
	previousFile = ""
	if prev != nil {
		previousFile = prev.LogoFile
	}
	if err := s.logoOverrides.Delete(ctx, ch.LibraryID, ch.StreamURL); err != nil {
		return "", err
	}
	return previousFile, nil
}

// GetChannelLogoOverride devuelve el override actual del canal (URL,
// archivo, o nil si no hay). El handler GET /channels/{id}/logo lo
// consulta para decidir entre servir desde disco (file) o pasar por el
// cache remoto (url o M3U).
func (s *Service) GetChannelLogoOverride(ctx context.Context, channelID string) (*iptvmodel.ChannelLogoOverride, error) {
	if s.logoOverrides == nil {
		return nil, nil
	}
	ch, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return s.logoOverrides.Get(ctx, ch.LibraryID, ch.StreamURL)
}
