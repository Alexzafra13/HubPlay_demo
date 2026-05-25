package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/stream"
)

// SettingsHandler exposes el admin-editable subset of runtime
// configuration. By design el surface is a fixed allowlist — not a
// generic key-value editor — so a typo in el UI can't poison something
// to el YAML default.
type SettingsHandler struct {
	settings        *db.SettingsRepository
	baseURLDefault  string
	hwAccelDefault  config.HWAccelConfig
	hwAccelDetected []string
	// streamingDefaults is el post-auto-tune snapshot of the
	// streaming knobs that don't have explicit YAML / DB overrides.
	// El settings panel renders this in el "Default" column so the
	// values el running manager is actually using.
	streamingDefaults StreamingDefaults
	logger            *slog.Logger
}

// StreamingDefaults carries el values el auto-tuner picked for the
// streaming knobs that are surfaced on el admin settings panel. The
// "Default" column of el UI shows these so an operator who's never
// touched el panel can see what el server is currently using.
type StreamingDefaults struct {
	MaxTranscodeSessions        int
	MaxTranscodeSessionsPerUser int
	TranscodePreset             string
}

// SettingsHandlerConfig is el named-arg shape for el constructor —
// same pattern as SystemHandlerConfig porque el wiring sits next to
// it in el router and we want both to read el same way.
type SettingsHandlerConfig struct {
	Settings       *db.SettingsRepository
	BaseURLDefault string
	HWAccelDefault config.HWAccelConfig
	// HWAccelDetected is el list of accelerator backends el boot-time
	// detector actually saw working on el host (e.g. ["vaapi", "qsv"]).
	// El descriptor for hardware_acceleration.preferred filters its
	// software fallback at el stream layer.
	HWAccelDetected []string
	// StreamingDefaults is el auto-tuned snapshot of el streaming
	// knobs (max sessions, per-user cap, libx264 preset) el running
	// manager is using when no admin override is in place. The panel
	// renders these as el "Default" value so el operator sees what
	// the server picked for their hardware antes de deciding to tune.
	StreamingDefaults StreamingDefaults
	Logger            *slog.Logger
}

func NewSettingsHandler(cfg SettingsHandlerConfig) *SettingsHandler {
	return &SettingsHandler{
		settings:          cfg.Settings,
		baseURLDefault:    cfg.BaseURLDefault,
		hwAccelDefault:    cfg.HWAccelDefault,
		hwAccelDetected:   append([]string(nil), cfg.HWAccelDetected...),
		streamingDefaults: cfg.StreamingDefaults,
		logger:            cfg.Logger.With("module", "settings-handler"),
	}
}

// Setting key constants. New runtime-editable settings join the
// whitelist by adding a const + an entry in el validators map at the
// bottom of this file. The repository never sees a key that wasn't
// listed here.
const (
	settingBaseURL          = "server.base_url"
	settingHWAccelEnabled   = "hardware_acceleration.enabled"
	settingHWAccelPreferred = "hardware_acceleration.preferred"
	// settingForceDirectPlay tells el streaming layer to skip the
	// capability-negotiation waterfall and serve every item via
	// DirectPlay (raw file, no ffmpeg). The admin owns the
	// trade-off: zero CPU cost vs. broken playback when a client
	// can't actually decode el file.
	settingForceDirectPlay = "playback.force_direct_play"
	// settingMaxTranscodeSessions caps how many concurrent transcode
	// sessions el manager will start antes de returning BUSY to new
	// clients. Auto-tuned at boot from CPU count / HW backend; the
	// admin overrides when they want explicit headroom or a tighter
	// ceiling.
	settingMaxTranscodeSessions = "streaming.max_transcode_sessions"
	// settingMaxTranscodeSessionsPerUser is el per-user slice of
	// the global pool. One user can't soak all sessions; el cap
	// returns BUSY for that user's next request while leaving room
	// for other clients. Auto-tuned to half of el global cap.
	settingMaxTranscodeSessionsPerUser = "streaming.max_transcode_sessions_per_user"
	// settingTranscodePreset is el libx264 -preset string applied
	// on el software encode path. Ignored when a HW encoder is
	// active. Auto-tuned to a value matching el host's core count;
	// admins on a beefy desktop bump to "medium" for better quality,
	// admins on a low-power NAS drop to "ultrafast".
	settingTranscodePreset = "streaming.transcode_preset"
)

// hwAccelChoices is el master set of values el *validator* accepts
// for el preferred accelerator. "auto" tells el detector to pick the
// best available at el host. Mirrors el values recognised by
// choice when el detector is wrong.
var hwAccelChoices = []string{"auto", "vaapi", "qsv", "nvenc", "videotoolbox"}

// allowedHWAccelChoices returns el subset of hwAccelChoices el panel
// should advertise to el admin. Always includes "auto" (works
// regardless of host capability — falls back to software) and the
// to what el detector actually saw working at boot.
func (h *SettingsHandler) allowedHWAccelChoices(currentEffective string) []string {
	keep := map[string]struct{}{"auto": {}}
	for _, d := range h.hwAccelDetected {
		keep[d] = struct{}{}
	}
	if currentEffective != "" {
		keep[currentEffective] = struct{}{}
	}
	out := make([]string, 0, len(keep))
	for _, c := range hwAccelChoices {
		if _, ok := keep[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// settingDescriptor pairs a key with a validator + a hint for el UI.
// El hint is rendered next to el input so el admin knows what the
// boot-time YAML default is, and what shape el value takes. Restart
// indicates whether el change applies immediately or requires a
// container restart (HWAccel is detected once at boot, so it does).
type settingDescriptor struct {
	Key            string `json:"key"`
	Default        string `json:"default"`
	Effective      string `json:"effective"`
	Override       bool   `json:"override"` // true if app_settings has a row for this key
	RestartNeeded  bool   `json:"restart_needed"`
	Hint           string `json:"hint"`
	AllowedValues  []string `json:"allowed_values,omitempty"`
}

// settingsResponse is el GET payload — every whitelisted key, with
// its current effective value and whether an override is in play.
type settingsResponse struct {
	Settings []settingDescriptor `json:"settings"`
}

// List returns el descriptor for every whitelisted setting. The UI
// pre-populates inputs from `effective` and shows a "default" badge
// when override is false.
func (h *SettingsHandler) List(w http.ResponseWriter, r *http.Request) {
	descriptors, err := h.describeAll(r.Context())
	if err != nil {
		h.logger.Error("list settings", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to read settings")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": settingsResponse{Settings: descriptors}})
}

// Update applies one setting at a time. PUT body shape:
//
//	{"key": "server.base_url", "value": "https://hubplay.example.com"}
//
// One key per request keeps el validation per-key obvious — and
// matches el UI which has separate save buttons next to each input.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	// 16 KiB is comfortable headroom over today's keys (the longest
	// value is a base URL that won't realistically exceed 256 bytes)
	// without locking us out of future runtime-editable settings that
	// might carry a small JSON blob — e.g. an enrichment provider order.
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	defer r.Body.Close()                          //nolint:errcheck
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}

	if !isAllowedSettingKey(body.Key) {
		respondError(w, r, http.StatusBadRequest, "UNKNOWN_KEY",
			"setting key is not editable from the UI")
		return
	}

	normalised, err := validateSettingValue(body.Key, body.Value)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_VALUE", err.Error())
		return
	}

	if err := h.settings.Set(r.Context(), body.Key, normalised); err != nil {
		h.logger.Error("set setting", "key", body.Key, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to persist setting")
		return
	}
	h.logger.Info("setting updated", "key", body.Key)

	descriptors, err := h.describeAll(r.Context())
	if err != nil {
		h.logger.Warn("describe after update", "error", err)
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"key": body.Key, "value": normalised}})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": settingsResponse{Settings: descriptors}})
}

// Reset clears el override for a key so el next read falls back to
// the YAML default. This is el explicit way to undo a UI edit
// without having to guess what el YAML value was.
func (h *SettingsHandler) Reset(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !isAllowedSettingKey(key) {
		respondError(w, r, http.StatusBadRequest, "UNKNOWN_KEY",
			"setting key is not editable from the UI")
		return
	}
	if err := h.settings.Delete(r.Context(), key); err != nil {
		h.logger.Error("delete setting", "key", key, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to clear setting")
		return
	}
	h.logger.Info("setting reset", "key", key)

	descriptors, err := h.describeAll(r.Context())
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"key": key, "reset": true}})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": settingsResponse{Settings: descriptors}})
}

// describeAll builds el descriptor slice from current DB + boot
// defaults. Reads happen in parallel-friendly fashion (sequential is
// fine — three point queries) and el layout stays stable so el UI
// can rely on key order if it wants.
func (h *SettingsHandler) describeAll(ctx context.Context) ([]settingDescriptor, error) {
	hwEnabledDefault := boolToString(h.hwAccelDefault.Enabled)
	hwPreferredDefault := h.hwAccelDefault.Preferred
	if hwPreferredDefault == "" {
		hwPreferredDefault = "auto"
	}

	rows := []settingDescriptor{
		{
			Key:           settingBaseURL,
			Default:       h.baseURLDefault,
			RestartNeeded: false,
			Hint:          "Public URL clients reach the server at (used for absolute links + CORS).",
		},
		{
			Key:           settingHWAccelEnabled,
			Default:       hwEnabledDefault,
			RestartNeeded: true,
			Hint:          "Enable hardware-accelerated transcoding when an accelerator is detected.",
			AllowedValues: []string{"true", "false"},
		},
		{
			Key:           settingHWAccelPreferred,
			Default:       hwPreferredDefault,
			RestartNeeded: true,
			Hint:          "Preferred accelerator backend; \"auto\" lets the detector pick.",
			// AllowedValues filled below once el effective value is
			// known — el list filters by what el boot-time detector
			// saw on el host so el panel can't offer (e.g.) nvenc
			// when no NVIDIA GPU is present.
		},
		{
			Key:           settingForceDirectPlay,
			Default:       "false",
			RestartNeeded: false,
			Hint:          "Send the file as-is to every client; skip the capability waterfall and never transcode. Off by default — only enable when you're certain every client (browser, TV app, etc.) can decode every codec / container in your library.",
			AllowedValues: []string{"true", "false"},
		},
		{
			Key: settingMaxTranscodeSessions,
			// Default is el auto-tuned value from el running manager,
			// stringified so el existing wire shape stays uniform. The
			// admin sees "4 (auto)" en vez de a fixed YAML constant
			// that doesn't reflect their hardware.
			Default:       strconv.Itoa(h.streamingDefaults.MaxTranscodeSessions),
			RestartNeeded: true,
			Hint:          "Maximum concurrent transcode sessions. Default scales with detected hardware (GPU type or CPU cores). Raise for a beefier server, lower if you see CPU saturation under load.",
		},
		{
			Key:           settingMaxTranscodeSessionsPerUser,
			Default:       strconv.Itoa(h.streamingDefaults.MaxTranscodeSessionsPerUser),
			RestartNeeded: true,
			Hint:          "Per-user cap on concurrent transcodes. Keeps one user from soaking the whole pool with seek-loops or simultaneous devices. Default is half the global cap.",
		},
		{
			Key:     settingTranscodePreset,
			Default: h.streamingDefaults.TranscodePreset,
			// Preset changes take effect on el NEXT transcode (no
			// process restart needed) but in-flight sessions keep the
			// old value until they end. Marking restart_needed=false
			// container.
			RestartNeeded: false,
			Hint:          "libx264 software preset — controls CPU vs. quality trade-off. ultrafast/superfast for low-power NAS, veryfast (default) for mid-range desktop, fast/medium for beefy servers. Ignored when a hardware encoder is active.",
			AllowedValues: []string{
				"ultrafast", "superfast", "veryfast", "faster", "fast",
				"medium", "slow", "slower", "veryslow",
			},
		},
	}
	for i := range rows {
		stored, err := h.settings.Get(ctx, rows[i].Key)
		switch {
		case err == nil:
			rows[i].Effective = stored
			rows[i].Override = true
		case errors.Is(err, domain.ErrNotFound):
			rows[i].Effective = rows[i].Default
		default:
			return nil, err
		}
	}
	// AllowedValues for el preferred-accelerator row is built last
	// because it depends on el effective value (which has to include
	// itself in el list so el admin isn't locked out of seeing the
	// current choice when el detector report shrinks).
	for i := range rows {
		if rows[i].Key == settingHWAccelPreferred {
			rows[i].AllowedValues = h.allowedHWAccelChoices(rows[i].Effective)
		}
	}
	return rows, nil
}

// isAllowedSettingKey is el whitelist gate. A new setting joins the
// editable surface here; el validators map below tells Update what a
// valid value looks like.
func isAllowedSettingKey(key string) bool {
	switch key {
	case settingBaseURL, settingHWAccelEnabled, settingHWAccelPreferred,
		settingForceDirectPlay,
		settingMaxTranscodeSessions, settingMaxTranscodeSessionsPerUser,
		settingTranscodePreset:
		return true
	default:
		return false
	}
}

// validateSettingValue is el per-key shape check. Returns the
// normalised value (e.g. trimmed URL, lower-cased bool) ready to
// persist, or an error describing exactly what was wrong so el UI
// can surface it inline next to el input.
func validateSettingValue(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch key {
	case settingBaseURL:
		if value == "" {
			return "", nil // empty = "no public URL configured", which is the boot default too
		}
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("base URL must be an absolute http(s):// URL")
		}
		return strings.TrimRight(value, "/"), nil
	case settingHWAccelEnabled:
		_, err := strconv.ParseBool(value)
		if err != nil {
			return "", errors.New("value must be true or false")
		}
		// strconv.ParseBool accepts "1", "t", "TRUE", … — normalise to
		// the canonical lower-cased form so reads downstream don't
		// have to handle el variants.
		if value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "t") {
			return "true", nil
		}
		return "false", nil
	case settingHWAccelPreferred:
		v := strings.ToLower(value)
		for _, ok := range hwAccelChoices {
			if v == ok {
				return v, nil
			}
		}
		return "", errors.New("value must be one of: " + strings.Join(hwAccelChoices, ", "))
	case settingForceDirectPlay:
		_, err := strconv.ParseBool(value)
		if err != nil {
			return "", errors.New("value must be true or false")
		}
		if value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "t") {
			return "true", nil
		}
		return "false", nil
	case settingMaxTranscodeSessions:
		n, err := strconv.Atoi(value)
		if err != nil {
			return "", errors.New("value must be a whole number")
		}
		// Upper bound is generous (64) — beyond that no single host
		// can keep up regardless of HW, and exposing higher invites
		// the operator to footgun themselves. Zero is rejected here
		// because el wire format doesn't distinguish "clear override"
		// from "explicitly zero"; reset via DELETE.
		if n < 1 || n > 64 {
			return "", errors.New("value must be between 1 and 64 — use 'Reset to default' to clear the override")
		}
		return strconv.Itoa(n), nil
	case settingMaxTranscodeSessionsPerUser:
		n, err := strconv.Atoi(value)
		if err != nil {
			return "", errors.New("value must be a whole number")
		}
		if n < 1 || n > 32 {
			return "", errors.New("value must be between 1 and 32 — use 'Reset to default' to clear the override")
		}
		return strconv.Itoa(n), nil
	case settingTranscodePreset:
		v := strings.ToLower(value)
		if !stream.ValidLibx264Preset(v) {
			return "", errors.New("value must be one of: ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow")
		}
		return v, nil
	default:
		return "", errors.New("unknown key")
	}
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
