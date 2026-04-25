package iptv

// Channel health — satisfies the ChannelHealthReporter interface so
// the stream proxy can persist probe outcomes without importing `db`.
// Each call is fire-and-forget from the proxy's point of view: we use
// a short-deadline background ctx so the DB write never outlives its
// reasonable bound even if the caller's ctx is about to expire, and
// we don't surface DB errors upward — a failed health write must not
// tear down a stream.

import (
	"context"
	"strings"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// channelHealthProbeTimeout caps how long the repo has to record one
// probe outcome. Keeping the upper bound tight stops a slow SQLite
// WAL flush from pinning the proxy's goroutine across several seconds
// of latency noise.
const channelHealthProbeTimeout = 2 * time.Second

// healthBucket maps a raw consecutive-failure count to the wire
// bucket the admin UI consumes. Mirrors handlers.deriveHealthStatus
// — duplicated here to avoid an import cycle (handlers → iptv).
// Keep both in sync; UnhealthyThreshold is the single tunable.
func healthBucket(consecutiveFailures int) string {
	switch {
	case consecutiveFailures <= 0:
		return "ok"
	case consecutiveFailures >= db.UnhealthyThreshold:
		return "dead"
	default:
		return "degraded"
	}
}

// RecordProbeSuccess satisfies iptv.ChannelHealthReporter. Logs on
// DB error and moves on — the stream proxy does not care whether we
// actually persisted the state.
func (s *Service) RecordProbeSuccess(ctx context.Context, channelID string) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeSuccess(ctx, channelID); err != nil {
		s.logger.Debug("record probe success", "channel", channelID, "error", err)
		return
	}
	s.maybePublishHealthChange(ctx, channelID)
}

// RecordProbeFailure satisfies iptv.ChannelHealthReporter. Translates
// the Go error into a short, user-safe string before persisting.
func (s *Service) RecordProbeFailure(ctx context.Context, channelID string, probeErr error) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeFailure(ctx, channelID, sanitiseProbeError(probeErr)); err != nil {
		s.logger.Debug("record probe failure", "channel", channelID, "error", err)
		return
	}
	s.maybePublishHealthChange(ctx, channelID)
}

// maybePublishHealthChange compares the channel's post-write bucket
// with the last bucket we published for it; if it changed, emits a
// ChannelHealthChanged event so SSE subscribers (admin dashboard)
// can invalidate caches without polling. No-ops cleanly when the
// event bus isn't wired (tests, partial bring-up).
//
// Why post-write read instead of in-memory math: probe and playback
// failures both increment the counter through different code paths
// (proxy probe, prober worker, /playback-failure beacon). Reading
// canonical state avoids racing on a derived count.
func (s *Service) maybePublishHealthChange(ctx context.Context, channelID string) {
	if s.bus == nil {
		return
	}
	ch, err := s.channels.GetByID(ctx, channelID)
	if err != nil || ch == nil {
		return
	}
	bucket := healthBucket(ch.ConsecutiveFailures)

	s.healthMu.Lock()
	prev, seen := s.lastKnownBucket[channelID]
	s.lastKnownBucket[channelID] = bucket
	s.healthMu.Unlock()

	// Bootstrap silence: the very first observation of a channel
	// after process start seeds the map but never publishes. Without
	// this, the first prober pass after a restart fires N events
	// (one per channel) and the admin UI sees an invalidation
	// stampede on the unhealthy-list query — pure noise, since the
	// UI's initial fetch on mount already reflects the truth.
	// Steady-state transitions still publish normally.
	if !seen {
		return
	}
	if prev == bucket {
		return
	}
	s.publish(event.Event{
		Type: event.ChannelHealthChanged,
		Data: map[string]any{
			"channel_id":           ch.ID,
			"library_id":           ch.LibraryID,
			"health_status":        bucket,
			"consecutive_failures": ch.ConsecutiveFailures,
		},
	})
}

// sanitiseProbeError trims repeated prefix wrapping and strips any
// obviously-transient noise so the admin UI shows the underlying
// cause ("no such host", "connection refused", "HTTP 403") rather
// than a nest of wrapper prefixes.
func sanitiseProbeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Strip our own wrapping prefixes ("connect: ", "fetch EPG: ") so
	// the useful underlying message is what lands in the DB.
	for _, prefix := range []string{"connect: ", "fetch: ", "read upstream: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// ListUnhealthyChannels returns channels whose consecutive-failure
// count is at or above the threshold. Threshold 0 uses the repo
// default (db.UnhealthyThreshold).
func (s *Service) ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*db.Channel, error) {
	return s.channels.ListUnhealthyByLibrary(ctx, libraryID, threshold)
}

// ResetChannelHealth clears the health state for one channel so it
// reappears in the user-facing list on next render. Used by the
// admin "marcar como OK" action.
func (s *Service) ResetChannelHealth(ctx context.Context, channelID string) error {
	if err := s.channels.ResetHealth(ctx, channelID); err != nil {
		return err
	}
	// Use a fresh background ctx so the publish isn't tied to the
	// caller's deadline (the reset already succeeded).
	pubCtx, cancel := context.WithTimeout(context.Background(), channelHealthProbeTimeout)
	defer cancel()
	s.maybePublishHealthChange(pubCtx, channelID)
	return nil
}
