package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// settingsRig wires the handler against a real settings repo backed by
// the in-memory test DB. Real DB on purpose — the handler's whole job
// is "round-trip through app_settings + return descriptors", so a fake
// repo would test the wrong thing.
type settingsRig struct {
	handler *SettingsHandler
	router  http.Handler
}

func newSettingsRig(t *testing.T, baseURLDefault string, hw config.HWAccelConfig) *settingsRig {
	t.Helper()
	return newSettingsRigWithDetected(t, baseURLDefault, hw, nil)
}

// newSettingsRigWithDetected is the variant that exercises the
// HW-accel detector gating — pass the list of accelerators a host
// would have reported at boot to drive the AllowedValues filter.
func newSettingsRigWithDetected(t *testing.T, baseURLDefault string, hw config.HWAccelConfig, detected []string) *settingsRig {
	t.Helper()
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)
	h := NewSettingsHandler(SettingsHandlerConfig{
		Settings:        repo,
		BaseURLDefault:  baseURLDefault,
		HWAccelDefault:  hw,
		HWAccelDetected: detected,
		Logger:          newQuietLogger(),
	})
	r := chi.NewRouter()
	r.Get("/admin/system/settings", h.List)
	r.Put("/admin/system/settings", h.Update)
	r.Delete("/admin/system/settings/{key}", h.Reset)
	return &settingsRig{handler: h, router: r}
}

func unwrapSettings(t *testing.T, body []byte) []settingDescriptor {
	t.Helper()
	var resp struct {
		Data settingsResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v\n%s", err, body)
	}
	return resp.Data.Settings
}

func TestSettings_List_DefaultsBeforeAnyOverride(t *testing.T) {
	rig := newSettingsRig(t, "https://yaml.example/", config.HWAccelConfig{Enabled: true, Preferred: "vaapi"})
	req := httptest.NewRequest(http.MethodGet, "/admin/system/settings", nil)
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	rows := unwrapSettings(t, rr.Body.Bytes())
	if len(rows) != 3 {
		t.Fatalf("expected 3 settings, got %d", len(rows))
	}
	for _, row := range rows {
		if row.Override {
			t.Errorf("fresh DB should report no overrides; %s says override=true", row.Key)
		}
		switch row.Key {
		case "server.base_url":
			if row.Effective != "https://yaml.example/" {
				t.Errorf("base_url effective: got %q want yaml default", row.Effective)
			}
		case "hardware_acceleration.enabled":
			if row.Effective != "true" {
				t.Errorf("hwaccel.enabled: got %q want true", row.Effective)
			}
			if !row.RestartNeeded {
				t.Errorf("hwaccel.enabled: should advertise restart_needed=true")
			}
		case "hardware_acceleration.preferred":
			if row.Effective != "vaapi" {
				t.Errorf("hwaccel.preferred: got %q want vaapi", row.Effective)
			}
		}
	}
}

func TestSettings_Update_PersistsAndReportsOverride(t *testing.T) {
	rig := newSettingsRig(t, "https://yaml.example/", config.HWAccelConfig{Enabled: false})

	req := httptest.NewRequest(http.MethodPut, "/admin/system/settings",
		strings.NewReader(`{"key":"server.base_url","value":"https://prod.example/"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	rows := unwrapSettings(t, rr.Body.Bytes())
	for _, row := range rows {
		if row.Key != "server.base_url" {
			continue
		}
		if !row.Override {
			t.Errorf("override should be true after Update")
		}
		if row.Effective != "https://prod.example" {
			t.Errorf("effective after update: got %q want trailing-slash-stripped value", row.Effective)
		}
	}

	// Settings GetOr from the repo should now return the override (the
	// path the rest of the codebase reads through).
	stored, err := db.NewSettingsRepository(testutil.NewTestDB(t)).GetOr(context.Background(), "missing", "fallback")
	if err != nil {
		t.Fatalf("unrelated GetOr failure: %v", err)
	}
	if stored != "fallback" {
		t.Errorf("fresh-DB GetOr should hit fallback path: got %q", stored)
	}
}

// Whitelist gate — UNKNOWN_KEY for anything outside the allowed set.
// This is the "not a generic KV store" guarantee. If it ever regresses
// the panel could persist arbitrary keys an attacker named in a forged
// request — even with admin auth, the gate is the second line.
func TestSettings_Update_RejectsUnknownKey(t *testing.T) {
	rig := newSettingsRig(t, "", config.HWAccelConfig{})
	req := httptest.NewRequest(http.MethodPut, "/admin/system/settings",
		strings.NewReader(`{"key":"server.bind","value":"0.0.0.0:9999"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown key; got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "UNKNOWN_KEY") {
		t.Errorf("expected UNKNOWN_KEY in body; got %s", rr.Body.String())
	}
}

func TestSettings_Update_ValidatesValueShape(t *testing.T) {
	rig := newSettingsRig(t, "", config.HWAccelConfig{})
	cases := []struct {
		name       string
		key, value string
	}{
		{"base_url not absolute", "server.base_url", "/relative/path"},
		{"base_url not http", "server.base_url", "ftp://example.com"},
		{"hwaccel.enabled not bool", "hardware_acceleration.enabled", "yesnomaybe"},
		{"hwaccel.preferred unknown", "hardware_acceleration.preferred", "magick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"key": tc.key, "value": tc.value})
			req := httptest.NewRequest(http.MethodPut, "/admin/system/settings", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			rig.router.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d body=%s", tc.name, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "INVALID_VALUE") {
				t.Errorf("%s: expected INVALID_VALUE: %s", tc.name, rr.Body.String())
			}
		})
	}
}

func TestSettings_Update_NormalisesValue(t *testing.T) {
	rig := newSettingsRig(t, "", config.HWAccelConfig{})
	// Trailing slash stripped, trailing whitespace trimmed — store the
	// normalised form so downstream string comparisons stay clean.
	req := httptest.NewRequest(http.MethodPut, "/admin/system/settings",
		strings.NewReader(`{"key":"server.base_url","value":"  https://prod.example/  "}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	for _, row := range unwrapSettings(t, rr.Body.Bytes()) {
		if row.Key == "server.base_url" && row.Effective != "https://prod.example" {
			t.Errorf("normalised effective: got %q want %q", row.Effective, "https://prod.example")
		}
	}

	// Hwaccel bool variants normalise to "true" / "false".
	req2 := httptest.NewRequest(http.MethodPut, "/admin/system/settings",
		strings.NewReader(`{"key":"hardware_acceleration.enabled","value":"1"}`))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	rig.router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("hwaccel update status: %d body=%s", rr2.Code, rr2.Body.String())
	}
	for _, row := range unwrapSettings(t, rr2.Body.Bytes()) {
		if row.Key == "hardware_acceleration.enabled" && row.Effective != "true" {
			t.Errorf("hwaccel normalised: got %q want true", row.Effective)
		}
	}
}

func TestSettings_Reset_ClearsOverride(t *testing.T) {
	rig := newSettingsRig(t, "https://yaml.example/", config.HWAccelConfig{Enabled: true, Preferred: "vaapi"})

	// First set
	put := httptest.NewRequest(http.MethodPut, "/admin/system/settings",
		strings.NewReader(`{"key":"server.base_url","value":"https://prod.example/"}`))
	put.Header.Set("Content-Type", "application/json")
	rig.router.ServeHTTP(httptest.NewRecorder(), put)

	// Then reset
	del := httptest.NewRequest(http.MethodDelete, "/admin/system/settings/server.base_url", nil)
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, del)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete status: %d body=%s", rr.Code, rr.Body.String())
	}
	for _, row := range unwrapSettings(t, rr.Body.Bytes()) {
		if row.Key != "server.base_url" {
			continue
		}
		if row.Override {
			t.Errorf("override still true after reset")
		}
		if row.Effective != "https://yaml.example/" {
			t.Errorf("after reset: got %q want yaml default", row.Effective)
		}
	}
}

func TestSettings_Reset_RejectsUnknownKey(t *testing.T) {
	rig := newSettingsRig(t, "", config.HWAccelConfig{})
	req := httptest.NewRequest(http.MethodDelete, "/admin/system/settings/server.bind", nil)
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown key; got %d", rr.Code)
	}
}

// The panel must only advertise accelerator backends the host actually
// supports. Two scenarios:
//
//   - Detector saw vaapi+qsv on the host: AllowedValues = [auto, vaapi, qsv].
//     A user can't accidentally select nvenc here and crash transcode.
//   - Detector saw nothing (host has no GPU, or ffmpeg's hwaccel probe
//     failed): AllowedValues collapses to just ["auto"], plus whatever
//     value is currently effective so the admin can still see + reset
//     the YAML override they have.
//
// Validator stays broad on purpose — see the comment on hwAccelChoices.
// A scripted PUT can still set any whitelist value; the *panel* just
// won't lead the operator to a broken choice.
func TestSettings_HWAccel_AllowedValues_FilteredByDetector(t *testing.T) {
	t.Run("detector saw vaapi+qsv", func(t *testing.T) {
		rig := newSettingsRigWithDetected(t, "", config.HWAccelConfig{Preferred: "auto"},
			[]string{"vaapi", "qsv"})
		req := httptest.NewRequest(http.MethodGet, "/admin/system/settings", nil)
		rr := httptest.NewRecorder()
		rig.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
		}
		var got []string
		for _, row := range unwrapSettings(t, rr.Body.Bytes()) {
			if row.Key == "hardware_acceleration.preferred" {
				got = row.AllowedValues
			}
		}
		want := []string{"auto", "vaapi", "qsv"}
		if !equalStringSlice(got, want) {
			t.Errorf("AllowedValues: got %v want %v", got, want)
		}
	})

	t.Run("detector saw nothing", func(t *testing.T) {
		rig := newSettingsRigWithDetected(t, "", config.HWAccelConfig{Preferred: "auto"}, nil)
		req := httptest.NewRequest(http.MethodGet, "/admin/system/settings", nil)
		rr := httptest.NewRecorder()
		rig.router.ServeHTTP(rr, req)
		var got []string
		for _, row := range unwrapSettings(t, rr.Body.Bytes()) {
			if row.Key == "hardware_acceleration.preferred" {
				got = row.AllowedValues
			}
		}
		want := []string{"auto"}
		if !equalStringSlice(got, want) {
			t.Errorf("empty detector should yield [auto]; got %v", got)
		}
	})

	t.Run("effective value not in detected list still surfaces", func(t *testing.T) {
		// Admin had nvenc set in YAML but moved the binary to a host with
		// no NVIDIA GPU. The current effective is "nvenc"; the detector
		// reports vaapi only. The panel must still include nvenc so the
		// admin can see the bad state and either reset or pick auto —
		// hiding it would leave them with no path to fix from the UI.
		rig := newSettingsRigWithDetected(t, "", config.HWAccelConfig{Preferred: "nvenc"},
			[]string{"vaapi"})
		req := httptest.NewRequest(http.MethodGet, "/admin/system/settings", nil)
		rr := httptest.NewRecorder()
		rig.router.ServeHTTP(rr, req)
		var got []string
		for _, row := range unwrapSettings(t, rr.Body.Bytes()) {
			if row.Key == "hardware_acceleration.preferred" {
				got = row.AllowedValues
			}
		}
		want := []string{"auto", "vaapi", "nvenc"}
		if !equalStringSlice(got, want) {
			t.Errorf("effective preserved: got %v want %v", got, want)
		}
	})
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
