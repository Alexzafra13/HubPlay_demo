package stream_test

import (
	"testing"

	"hubplay/internal/config"
	"hubplay/internal/stream"
)

// TestRecommendMaxSessions pins the per-accelerator defaults so a
// silent change in the recommendation table can't break operator
// expectations after an upgrade — the auto-tuned value shows in the
// admin UI and is therefore part of the user contract.
func TestRecommendMaxSessions(t *testing.T) {
	tests := []struct {
		name     string
		hw       stream.HWAccelType
		cpuCount int
		want     int
	}{
		{"nvenc consumer", stream.HWAccelNVENC, 16, 3},
		{"qsv intel igpu", stream.HWAccelQSV, 4, 6},
		{"vaapi intel/amd", stream.HWAccelVAAPI, 8, 6},
		{"videotoolbox apple", stream.HWAccelVideoToolbox, 8, 4},
		{"software 16 cores", stream.HWAccelNone, 16, 8},
		{"software 8 cores", stream.HWAccelNone, 8, 4},
		{"software 4 cores", stream.HWAccelNone, 4, 2},
		{"software 2 cores", stream.HWAccelNone, 2, 1},
		{"software 1 core", stream.HWAccelNone, 1, 1},
		{"software 0 cores defends against bad input", stream.HWAccelNone, 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stream.RecommendMaxSessions(tc.hw, tc.cpuCount)
			if got != tc.want {
				t.Errorf("RecommendMaxSessions(%v, %d) = %d, want %d",
					tc.hw, tc.cpuCount, got, tc.want)
			}
		})
	}
}

// TestRecommendPerUserCap covers the "half rounded up, min 1"
// contract. The +1 odd-handling is the part that's easy to get
// wrong if someone refactors with bit-shifts.
func TestRecommendPerUserCap(t *testing.T) {
	tests := []struct {
		global int
		want   int
	}{
		{1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 3},
		{6, 3}, {7, 4}, {8, 4}, {0, 1}, {-1, 1},
	}
	for _, tc := range tests {
		got := stream.RecommendPerUserCap(tc.global)
		if got != tc.want {
			t.Errorf("RecommendPerUserCap(%d) = %d, want %d", tc.global, got, tc.want)
		}
	}
}

// TestRecommendPreset_HWAccel pins that all HW backends collapse to
// "veryfast". libx264 ignores -preset on HW, but the value is read
// from cfg unconditionally for the UI display + audit trail.
func TestRecommendPreset_HWAccel(t *testing.T) {
	for _, hw := range []stream.HWAccelType{
		stream.HWAccelNVENC, stream.HWAccelQSV,
		stream.HWAccelVAAPI, stream.HWAccelVideoToolbox,
	} {
		if got := stream.RecommendPreset(hw, 2); got != "veryfast" {
			t.Errorf("RecommendPreset(%v, _) = %q, want veryfast", hw, got)
		}
	}
}

// TestRecommendPreset_Software walks the core-count ladder. The
// boundaries matter: 4 must land on superfast (not ultrafast),
// 12 on fast (not veryfast).
func TestRecommendPreset_Software(t *testing.T) {
	tests := []struct {
		cores int
		want  string
	}{
		{1, "ultrafast"}, {3, "ultrafast"},
		{4, "superfast"}, {5, "superfast"},
		{6, "veryfast"}, {11, "veryfast"},
		{12, "fast"}, {32, "fast"},
	}
	for _, tc := range tests {
		got := stream.RecommendPreset(stream.HWAccelNone, tc.cores)
		if got != tc.want {
			t.Errorf("RecommendPreset(none, %d) = %q, want %q", tc.cores, got, tc.want)
		}
	}
}

// TestAutoTuneStreaming_FillsZeros verifies the core invariant:
// zero/empty values get filled, non-zero values pass through. This
// is what protects an admin's deliberate override from being
// overwritten on the next process restart.
func TestAutoTuneStreaming_FillsZeros(t *testing.T) {
	in := config.StreamingConfig{
		MaxTranscodeSessions:        0,
		MaxTranscodeSessionsPerUser: 0,
		TranscodePreset:             "",
	}
	out := stream.AutoTuneStreaming(in, stream.HWAccelNone, 8)
	if out.MaxTranscodeSessions != 4 {
		t.Errorf("MaxTranscodeSessions = %d, want 4 (8 cores / 2)", out.MaxTranscodeSessions)
	}
	if out.MaxTranscodeSessionsPerUser != 2 {
		t.Errorf("MaxTranscodeSessionsPerUser = %d, want 2 (4 / 2)", out.MaxTranscodeSessionsPerUser)
	}
	if out.TranscodePreset != "veryfast" {
		t.Errorf("TranscodePreset = %q, want veryfast (8 cores)", out.TranscodePreset)
	}
}

func TestAutoTuneStreaming_PreservesExplicitValues(t *testing.T) {
	in := config.StreamingConfig{
		MaxTranscodeSessions:        12,
		MaxTranscodeSessionsPerUser: 4,
		TranscodePreset:             "medium",
	}
	out := stream.AutoTuneStreaming(in, stream.HWAccelNone, 2)
	// Admin override on a 2-core box (sub-optimal but explicit) MUST
	// survive auto-tune unchanged — the operator owns the trade-off.
	if out.MaxTranscodeSessions != 12 {
		t.Errorf("MaxTranscodeSessions clobbered: got %d, want 12", out.MaxTranscodeSessions)
	}
	if out.MaxTranscodeSessionsPerUser != 4 {
		t.Errorf("MaxTranscodeSessionsPerUser clobbered: got %d, want 4", out.MaxTranscodeSessionsPerUser)
	}
	if out.TranscodePreset != "medium" {
		t.Errorf("TranscodePreset clobbered: got %q, want medium", out.TranscodePreset)
	}
}

func TestAutoTuneStreaming_PartialOverride(t *testing.T) {
	// Admin sets just the global cap; per-user + preset stay auto.
	in := config.StreamingConfig{MaxTranscodeSessions: 10}
	out := stream.AutoTuneStreaming(in, stream.HWAccelNone, 8)
	if out.MaxTranscodeSessions != 10 {
		t.Errorf("global override lost: %d", out.MaxTranscodeSessions)
	}
	// per-user derives from the (now 10) global, not the recommendation
	if out.MaxTranscodeSessionsPerUser != 5 {
		t.Errorf("per-user should derive from override global: got %d, want 5",
			out.MaxTranscodeSessionsPerUser)
	}
	if out.TranscodePreset != "veryfast" {
		t.Errorf("preset should auto-tune: got %q", out.TranscodePreset)
	}
}

func TestValidLibx264Preset(t *testing.T) {
	for _, ok := range []string{
		"ultrafast", "superfast", "veryfast", "faster", "fast",
		"medium", "slow", "slower", "veryslow",
	} {
		if !stream.ValidLibx264Preset(ok) {
			t.Errorf("ValidLibx264Preset(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "fastest", "placebo", "ULTRAFAST", "x264-veryfast"} {
		if stream.ValidLibx264Preset(bad) {
			t.Errorf("ValidLibx264Preset(%q) = true, want false", bad)
		}
	}
}
