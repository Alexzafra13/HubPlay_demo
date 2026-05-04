package federation

import "time"

// Progress is one user's playback state for a remote item on a paired
// peer. Federated items never live in the local `items` table, so we
// keep peer playback state in its own surface (federation_progress)
// rather than trying to fit them into user_data. Schema lives in
// migrations/sqlite/028_federation_progress.sql.
//
// DurationTicks may be 0 on the very first save -- the player learns
// duration from the manifest after a few segments. The repository
// upsert preserves a previously-stored non-zero duration when the
// caller passes 0, so the Continue Watching rail's percentage stays
// stable as soon as duration is known once.
type Progress struct {
	UserID        string
	PeerID        string
	RemoteItemID  string
	PositionTicks int64
	DurationTicks int64
	Completed     bool
	LastPlayedAt  time.Time
	UpdatedAt     time.Time
}

// PeerContinueWatchingItem is one row of the cross-peer Continue
// Watching rail: the playback state plus enough catalog metadata
// (joined from federation_item_cache + federation_peers) to render
// a card without a per-row hop.
type PeerContinueWatchingItem struct {
	PeerID        string
	PeerName      string
	LibraryID     string
	RemoteItemID  string
	Type          string
	Title         string
	Year          int
	Overview      string
	HasPoster     bool
	PositionTicks int64
	DurationTicks int64
	LastPlayedAt  time.Time
}
