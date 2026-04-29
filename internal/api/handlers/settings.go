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
)

// SettingsHandler exposes the admin-editable subset of runtime
// configuration. By design the surface is a fixed allowlist — not a
// generic key-value editor — so a typo in the UI can't poison something
// the operator should be touching the YAML file for.
//
// The split with config.Config is:
//   - YAML / env stays the source of truth for boot-time values
//     (bind, port, paths, secrets, dev origins).
//   - The keys here are the runtime-mutable preferences. A row in
//     app_settings overrides the YAML default; deleting the row reverts
//     to the YAML default.
type SettingsHandler struct {
	settings         *db.SettingsRepository
	baseURLDefault   string
	hwAccelDefault   config.HWAccelConfig
	hwAccelDetected  []string
	logger           *slog.Logger
}

// SettingsHandlerConfig is the named-arg shape for the constructor —
// same pattern as SystemHandlerConfig because the wiring sits next to
// it in the router and we want both to read the same way.
type SettingsHandlerConfig struct {
	Settings       *db.SettingsRepository
	BaseURLDefault string
	HWAccelDefault config.HWAccelConfig
	// HWAccelDetected is the list of accelerator backends the boot-time
	// detector actually saw working on the host (e.g. ["vaapi", "qsv"]).
	// The descriptor for hardware_acceleration.preferred filters its
	// AllowedValues against this list so the UI only offers choices
	// that have a chance of working — the operator can't flip nvenc on
	// a host with no NVIDIA GPU and crash the next transcode.
	// Empty / nil means "detector saw nothing" (or wasn't wired); in
	// that case the panel only offers "auto", which still maps to the
	// software fallback at the stream layer.
	HWAccelDetected []string
	Logger          *slog.Logger
}

func NewSettingsHandler(cfg SettingsHandlerConfig) *SettingsHandler {
	return &SettingsHandler{
		settings:        cfg.Settings,
		baseURLDefault:  cfg.BaseURLDefault,
		hwAccelDefault:  cfg.HWAccelDefault,
		hwAccelDetected: append([]string(nil), cfg.HWAccelDetected...),
		logger:          cfg.Logger.With("module", "settings-handler"),
	}
}

// Setting key constants. New runtime-editable settings join the
// whitelist by adding a const + an entry in the validators map at the
// bottom of this file. The repository never sees a key that wasn't
// listed here.
const (
	settingBaseURL          = "server.base_url"
	settingHWAccelEnabled   = "hardware_acceleration.enabled"
	settingHWAccelPreferred = "hardware_acceleration.preferred"
)

// hwAccelChoices is the master set of values the *validator* accepts
// for the preferred accelerator. "auto" tells the detector to pick the
// best available at the host. Mirrors the values recognised by
// stream.DetectHWAccel — keep in sync.
//
// Note: the panel's UI advertises a *narrower* list, filtered by what
// the boot detector actually saw on the host (see allowedHWAccelChoices).
// We deliberately keep the validator broad so an operator who knows
// what they're doing can still set, say, `nvenc` from a CLI / scripted
// API call when the detector failed silently — without having to touch
// YAML. The cost of that flexibility is a single misconfigured value
// surfacing at next transcode; the gain is the panel never *forces* a
// choice when the detector is wrong.
var hwAccelChoices = []string{"auto", "vaapi", "qsv", "nvenc", "videotoolbox"}

// allowedHWAccelChoices returns the subset of hwAccelChoices the panel
// should advertise to the admin. Always includes "auto" (works
// regardless of host capability — falls back to software) and the
// currently-effective preferred value (so an admin who already has
// a value set in YAML or in app_settings can see + reset it, even if
// the detector now reports the host can't run it). Otherwise restricts
// to what the detector actually saw working at boot.
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

// settingDescriptor pairs a key with a validator + a hint for the UI.
// The hint is rendered next to the input so the admin knows what the
// boot-time YAML default is, and what shape the value takes. Restart
// indicates whether the change applies immediately or requires a
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

// settingsResponse is the GET payload — every whitelisted key, with
// its current effective value and whether an override is in play.
type settingsResponse struct {
	Settings []settingDescriptor `json:"settings"`
}

// List returns the descriptor for every whitelisted setting. The UI
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
// One key per request keeps the validation per-key obvious — and
// matches the UI which has separate save buttons next to each input.
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

// Reset clears the override for a key so the next read falls back to
// the YAML default. This is the explicit way to undo a UI edit
// without having to guess what the YAML value was.
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

// describeAll builds the descriptor slice from current DB + boot
// defaults. Reads happen in parallel-friendly fashion (sequential is
// fine — three point queries) and the layout stays stable so the UI
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
			// AllowedValues filled below once the effective value is
			// known — the list filters by what the boot-time detector
			// saw on the host so the panel can't offer (e.g.) nvenc
			// when no NVIDIA GPU is present.
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
	// AllowedValues for the preferred-accelerator row is built last
	// because it depends on the effective value (which has to include
	// itself in the list so the admin isn't locked out of seeing the
	// current choice when the detector report shrinks).
	for i := range rows {
		if rows[i].Key == settingHWAccelPreferred {
			rows[i].AllowedValues = h.allowedHWAccelChoices(rows[i].Effective)
		}
	}
	return rows, nil
}

// isAllowedSettingKey is the whitelist gate. A new setting joins the
// editable surface here; the validators map below tells Update what a
// valid value looks like.
func isAllowedSettingKey(key string) bool {
	switch key {
	case settingBaseURL, settingHWAccelEnabled, settingHWAccelPreferred:
		return true
	default:
		return false
	}
}

// validateSettingValue is the per-key shape check. Returns the
// normalised value (e.g. trimmed URL, lower-cased bool) ready to
// persist, or an error describing exactly what was wrong so the UI
// can surface it inline next to the input.
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
		// have to handle the variants.
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
