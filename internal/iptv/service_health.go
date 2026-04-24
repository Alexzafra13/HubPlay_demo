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
)

// channelHealthProbeTimeout caps how long the repo has to record one
// probe outcome. Keeping the upper bound tight stops a slow SQLite
// WAL flush from pinning the proxy's goroutine across several seconds
// of latency noise.
const channelHealthProbeTimeout = 2 * time.Second

// RecordProbeSuccess satisfies iptv.ChannelHealthReporter. Logs on
// DB error and moves on — the stream proxy does not care whether we
// actually persisted the state.
func (s *Service) RecordProbeSuccess(ctx context.Context, channelID string) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeSuccess(ctx, channelID); err != nil {
		s.logger.Debug("record probe success", "channel", channelID, "error", err)
	}
}

// RecordProbeFailure satisfies iptv.ChannelHealthReporter. Translates
// the Go error into a short, user-safe string before persisting.
func (s *Service) RecordProbeFailure(ctx context.Context, channelID string, probeErr error) {
	ctx, cancel := context.WithTimeout(ctx, channelHealthProbeTimeout)
	defer cancel()
	if err := s.channels.RecordProbeFailure(ctx, channelID, sanitiseProbeError(probeErr)); err != nil {
		s.logger.Debug("record probe failure", "channel", channelID, "error", err)
	}
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
	return s.channels.ResetHealth(ctx, channelID)
}
