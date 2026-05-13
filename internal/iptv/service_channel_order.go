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

	"hubplay/internal/db"
)

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
func applyOrderOverlay(channels []*db.Channel, overrides []db.UserChannelOrderEntry) []*db.Channel {
	if len(overrides) == 0 {
		return channels
	}
	byID := make(map[string]db.UserChannelOrderEntry, len(overrides))
	for _, o := range overrides {
		byID[o.ChannelID] = o
	}

	out := make([]*db.Channel, 0, len(channels))
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
func (s *Service) GetChannelsForUser(ctx context.Context, libraryID, userID string, activeOnly bool) ([]*db.Channel, error) {
	channels, err := s.GetChannels(ctx, libraryID, activeOnly)
	if err != nil {
		return nil, err
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

// ListChannelOverrides returns the user's raw override rows for the
// personalisation panel. The panel renders these alongside the
// channel list so the user can see which channels they've touched
// (highlighted) vs. which still inherit the admin defaults.
func (s *Service) ListChannelOverrides(ctx context.Context, userID string) ([]db.UserChannelOrderEntry, error) {
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
	entries := make([]db.UserChannelOrderEntry, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entries = append(entries, db.UserChannelOrderEntry{
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
