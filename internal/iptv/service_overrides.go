package iptv

// Manual channel editing (admin) — surface for the "canales sin guía"
// panel. The admin patches a channel's tvg_id to force a match against
// a specific XMLTV entry (e.g. davidmuma uses "La 1 HD" where the
// iptv-org M3U has "La1.es@SD"). The change is applied immediately to
// `channels` AND persisted to `channel_overrides` so it survives the
// next M3U refresh.

import (
	"context"
	"fmt"
	"time"

	"hubplay/internal/db"
)

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
