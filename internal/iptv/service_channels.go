package iptv

// Channel read/write surface. User-facing reads use the healthy list
// (auto-hides channels over the unhealthy threshold); admin callers
// pass activeOnly=false to see the full set including dead tiles.

import (
	"context"

	"hubplay/internal/db"
)

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

// SetChannelActive enables or disables a channel.
func (s *Service) SetChannelActive(ctx context.Context, id string, active bool) error {
	return s.channels.SetActive(ctx, id, active)
}
