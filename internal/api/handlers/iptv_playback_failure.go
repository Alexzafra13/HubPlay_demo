package handlers

// Player-side failure beacon. The HLS player (hls.js) reports fatal
// errors here so the same channel-health pipeline that the server-
// side proxy already drives also captures the "manifest 200 OK but
// segments are dead" case the proxy can't see (the proxy succeeds at
// fetching the manifest, but the player then fails to decode the TS
// fragments). After three consecutive failures from any source the
// channel drops out of the user-facing list — same threshold as the
// proxy path.
//
// ACL: same shape as RecordChannelWatch — any authenticated user can
// flag a channel they have library access to. Without the access
// gate a hostile client could push every channel into the dead
// bucket; with it, the worst a single user can do is hide channels
// from themselves (the threshold is consecutive failures across all
// callers, but a real user opening the channel resets the counter
// via RecordProbeSuccess on the next successful proxy fetch).
//
// Rate-limit: one beacon per (user, channel) per playbackBeaconCooldown
// keeps a flapping player from incrementing the failure counter on
// every retry. Enforced in-memory — the data is throwaway and a
// process restart resetting the cooldown is fine.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
)


const (
	// playbackBeaconCooldown is the per-(user,channel) minimum gap
	// between accepted beacons. Picked to absorb hls.js's exponential
	// retry sequence (typically 1s, 2s, 4s before fatal) so we record
	// at most one failure per real "the user pressed play and it
	// didn't work" event.
	playbackBeaconCooldown = 30 * time.Second

	// playbackBeaconMaxBody caps the request body. The schema is
	// tiny ({"kind":"…","details":"…"}) — anything larger is
	// either a misbehaving client or an attempt to fill the DB
	// with junk error strings.
	playbackBeaconMaxBody = 2 << 10 // 2 KB

	// playbackDetailsMaxLen trims the optional details string so
	// nothing exotic from the player ends up in the DB.
	playbackDetailsMaxLen = 200
)

// playbackBeaconCooldowns is the per-process dedup map. Concurrent
// access via the proxy package's stream sharing means we want a
// pointer-friendly cache, not a per-handler one.
var (
	playbackBeaconMu     sync.Mutex
	playbackBeaconLastAt = make(map[string]time.Time)
)

// allowedPlaybackKinds enumerates the buckets the frontend can pass.
// Locked to a small enum so a misbehaving client can't stuff arbitrary
// strings into the DB error column.
var allowedPlaybackKinds = map[string]struct{}{
	"manifest":  {}, // ERROR_MANIFEST_LOAD_ERROR / parse error
	"network":   {}, // segment fetch failed
	"media":     {}, // codec / decoder error
	"timeout":   {}, // hls.js fragLoadTimeOut
	"unknown":   {}, // catch-all so the client doesn't have to classify
}

type playbackFailureRequest struct {
	Kind    string `json:"kind"`
	Details string `json:"details,omitempty"`
}

// RecordPlaybackFailure handles POST /api/v1/channels/{channelId}/playback-failure.
// On accepted beacons it forwards a synthetic error to the existing
// ChannelHealthReporter pipeline so the same `consecutive_failures`
// counter the proxy uses bumps by one — keeping the dead-channel
// machinery single-sourced.
func (h *IPTVHandler) RecordPlaybackFailure(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "auth required")
		return
	}
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	body := playbackFailureRequest{Kind: "unknown"}
	if r.ContentLength > 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, playbackBeaconMaxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return
		}
	}

	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	if kind == "" {
		kind = "unknown"
	}
	if _, ok := allowedPlaybackKinds[kind]; !ok {
		respondError(w, r, http.StatusBadRequest, "INVALID_KIND",
			fmt.Sprintf("unknown kind %q", kind))
		return
	}

	// Cooldown gate. Returns 202 (accepted but not acted on) so a
	// flapping client doesn't think its beacon was lost.
	cacheKey := claims.UserID + "|" + channelID
	playbackBeaconMu.Lock()
	lastAt, seen := playbackBeaconLastAt[cacheKey]
	now := time.Now()
	if seen && now.Sub(lastAt) < playbackBeaconCooldown {
		playbackBeaconMu.Unlock()
		respondJSON(w, http.StatusAccepted, map[string]any{
			"data": map[string]any{
				"channel_id": channelID,
				"recorded":   false,
				"reason":     "cooldown",
			},
		})
		return
	}
	playbackBeaconLastAt[cacheKey] = now
	playbackBeaconMu.Unlock()

	details := strings.TrimSpace(body.Details)
	if len(details) > playbackDetailsMaxLen {
		details = details[:playbackDetailsMaxLen]
	}
	msg := "player: " + kind
	if details != "" {
		msg += " (" + details + ")"
	}

	// Use the same ChannelHealthReporter API the proxy uses so the
	// counter / list-healthy-vs-unhealthy plumbing stays single-
	// sourced. Pass a synthetic error so sanitiseProbeError trims
	// it consistently with the proxy path.
	h.svc.RecordProbeFailure(r.Context(), channelID, errors.New(msg))

	// Re-fetch to surface the new health bucket to the client so it
	// can update local state without a separate round-trip.
	updated, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		// Beacon already recorded — surface a partial response
		// rather than fail the whole call.
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"channel_id": channelID,
				"recorded":   true,
			},
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"channel_id":           channelID,
			"recorded":             true,
			"consecutive_failures": updated.ConsecutiveFailures,
			"health_status":        deriveHealthStatus(updated),
			"unhealthy_threshold":  db.UnhealthyThreshold,
		},
	})
}
