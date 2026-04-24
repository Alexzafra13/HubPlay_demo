package iptv

// Channel watch-history surface. The beacon from the player calls
// RecordWatch with a channel id; the Discover rail calls
// ListContinueWatching. The service translates between channel-id
// (what the UI holds) and stream-url (what the DB keys on) — see
// migration 012 for why keying is stream_url.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db"
)

// RecordWatch upserts a (user, channel) pair in the watch history.
// Resolves the channel's stream_url first so the history row survives
// the next M3U refresh (UUIDs change, URLs don't).
//
// Returns the timestamp written so the HTTP handler can echo it to
// the client without a second read.
//
// Returns ErrChannelNotFound if the channel has been dropped from the
// library since the beacon was triggered. The caller should translate
// this into 404 — a race between a channel removal and a pending
// beacon should not surface as a server error.
func (s *Service) RecordWatch(ctx context.Context, userID, channelID string) (time.Time, error) {
	if s.watchHistory == nil {
		return time.Time{}, fmt.Errorf("watch history not configured")
	}
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		if errors.Is(err, db.ErrChannelNotFound) {
			return time.Time{}, err
		}
		return time.Time{}, fmt.Errorf("get channel: %w", err)
	}
	return s.watchHistory.RecordByStreamURL(ctx, userID, ch.StreamURL)
}

// ListContinueWatching returns the user's most recently watched
// channels, newest first, capped at limit. Library ACL filtering is
// NOT applied here — the caller is expected to pass the filter based
// on request context. Callers for the admin surface (where admin sees
// all libraries) can skip the filter entirely.
//
// accessibleLibraries nil = no filtering; empty map = deny everything;
// a populated map keeps only channels whose LibraryID is a key.
func (s *Service) ListContinueWatching(
	ctx context.Context,
	userID string,
	limit int,
	accessibleLibraries map[string]bool,
) ([]*db.Channel, []time.Time, error) {
	if s.watchHistory == nil {
		return nil, nil, nil
	}
	// Fetch a few extras so the post-filter (access) still returns up
	// to `limit` — pathological access denials would otherwise
	// truncate the rail below its intended size.
	fetch := limit
	if accessibleLibraries != nil {
		fetch = limit * 2
		if fetch < 10 {
			fetch = 10
		}
	}
	channels, watched, err := s.watchHistory.ListChannelsByUser(ctx, userID, fetch)
	if err != nil {
		return nil, nil, err
	}
	if accessibleLibraries == nil {
		if len(channels) > limit {
			channels = channels[:limit]
			watched = watched[:limit]
		}
		return channels, watched, nil
	}
	outCh := make([]*db.Channel, 0, len(channels))
	outTs := make([]time.Time, 0, len(channels))
	for i, ch := range channels {
		if accessibleLibraries[ch.LibraryID] {
			outCh = append(outCh, ch)
			outTs = append(outTs, watched[i])
			if len(outCh) >= limit {
				break
			}
		}
	}
	return outCh, outTs, nil
}
