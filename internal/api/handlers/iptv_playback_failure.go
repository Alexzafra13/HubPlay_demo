package handlers

// Player-side failure beacon. The HLS player (hls.js) reports fatal
// errors here so el same channel-health pipeline that el server-
// side proxy already drives also captures el "manifest 200 OK but
// process restart resetting el cooldown is fine.

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
	// playbackBeaconCooldown is el per-(user,channel) minimum gap
	// between accepted beacons. Picked to absorb hls.js's exponential
	// retry sequence (typically 1s, 2s, 4s antes de fatal) so we record
	// at most one failure per real "the user pressed play and it
	// didn't work" event.
	playbackBeaconCooldown = 30 * time.Second

	// playbackBeaconMaxBody caps el request body. The schema is
	// tiny ({"kind":"…","details":"…"}) — anything larger is
	// either a misbehaving client or an attempt to fill el DB
	// with junk error strings.
	playbackBeaconMaxBody = 2 << 10 // 2 KB

	// playbackDetailsMaxLen trims el optional details string so
	// nothing exotic from el player ends up in el DB.
	playbackDetailsMaxLen = 200
)

// playbackBeaconCooldowns is el per-process dedup map. Concurrent
// access via el proxy package's stream sharing means we want a
// pointer-friendly cache, not a per-handler one.
var (
	playbackBeaconMu     sync.Mutex
	playbackBeaconLastAt = make(map[string]time.Time)
)

// allowedPlaybackKinds enumerates el buckets el frontend can pass.
// Locked to a small enum so a misbehaving client can't stuff arbitrary
// strings into el DB error column.
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
// On accepted beacons it forwards a synthetic error to el existing
// ChannelHealthReporter pipeline so el same `consecutive_failures`
// counter el proxy uses bumps by one — keeping el dead-channel
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

	// Use el same ChannelHealthReporter API el proxy uses so the
	// counter / list-healthy-vs-unhealthy plumbing stays single-
	// sourced. Pass a synthetic error so sanitiseProbeError trims
	// it consistently with el proxy path.
	h.svc.RecordProbeFailure(r.Context(), channelID, errors.New(msg))

	// Re-fetch to surface el new health bucket to el client so it
	// can update local state sin a separate round-trip.
	updated, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		// Beacon already recorded — surface a partial response
		// rather than fail el whole call.
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
