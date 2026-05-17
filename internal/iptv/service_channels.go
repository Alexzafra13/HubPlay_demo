package iptv

// Channel read/write surface. User-facing reads use the healthy list
// (auto-hides channels over the unhealthy threshold); admin callers
// pass activeOnly=false to see the full set including dead tiles.

import (
	"context"

	iptvmodel "hubplay/internal/iptv/model"
)

// GetChannels returns channels for a library.
//
// When activeOnly is true (the default for user-facing surfaces) the
// list is also filtered for health: channels that have failed the
// probe UnhealthyThreshold times in a row are hidden so viewers
// don't click dead tiles. Admin callers pass activeOnly=false and
// get the full set including disabled and unhealthy rows.
func (s *Service) GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*iptvmodel.Channel, error) {
	if activeOnly {
		return s.channels.ListHealthyByLibrary(ctx, libraryID)
	}
	return s.channels.ListByLibrary(ctx, libraryID, false)
}

// GetChannel returns a single channel by ID.
func (s *Service) GetChannel(ctx context.Context, id string) (*iptvmodel.Channel, error) {
	return s.channels.GetByID(ctx, id)
}

// GetGroups returns channel group names for a library.
//
// Output is normalised + deduplicated: the M3U importer now strips
// multi-token group-titles ("Animation;Kids;Public" → "Animation")
// before insert, but legacy rows from older builds may still carry
// the packed form. Cleaning at read time means existing libraries
// surface tidy chips without needing a re-import or one-shot
// migration.
func (s *Service) GetGroups(ctx context.Context, libraryID string) ([]string, error) {
	raw, err := s.channels.Groups(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(raw))
	out  := make([]string, 0, len(raw))
	for _, g := range raw {
		n := NormalizeGroupTitle(g)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out, nil
}

// SetChannelActive enables or disables a channel.
func (s *Service) SetChannelActive(ctx context.Context, id string, active bool) error {
	return s.channels.SetActive(ctx, id, active)
}
