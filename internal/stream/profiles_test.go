package stream_test

import (
	"testing"

	"hubplay/internal/stream"
)

func TestProfiles_AllDefined(t *testing.T) {
	names := stream.ProfileNames()
	if len(names) != 5 {
		t.Fatalf("expected 5 profiles, got %d", len(names))
	}

	for _, name := range names {
		p, ok := stream.Profiles[name]
		if !ok {
			t.Errorf("profile %q listed in ProfileNames() but not in Profiles map", name)
		}
		if p.Name != name {
			t.Errorf("profile %q has mismatched Name field %q", name, p.Name)
		}
	}
}

func TestProfiles_ResolutionDecreasing(t *testing.T) {
	order := []string{"1080p", "720p", "480p", "360p"}
	for i := 1; i < len(order); i++ {
		prev := stream.Profiles[order[i-1]]
		curr := stream.Profiles[order[i]]
		if curr.Width >= prev.Width || curr.Height >= prev.Height {
			t.Errorf("%s (%dx%d) should be smaller than %s (%dx%d)",
				curr.Name, curr.Width, curr.Height, prev.Name, prev.Width, prev.Height)
		}
	}
}

func TestProfiles_OriginalHasNoEncoding(t *testing.T) {
	p := stream.Profiles["original"]
	if p.Width != 0 || p.Height != 0 {
		t.Error("original profile should have zero dimensions (direct play)")
	}
	if p.VideoBitrate != "" || p.AudioBitrate != "" {
		t.Error("original profile should have no bitrate settings")
	}
}

func TestProfiles_TranscodingHaveRequiredFields(t *testing.T) {
	for _, name := range []string{"1080p", "720p", "480p", "360p"} {
		p := stream.Profiles[name]
		if p.Width == 0 || p.Height == 0 {
			t.Errorf("%s: width and height must be set", name)
		}
		if p.VideoBitrate == "" {
			t.Errorf("%s: video bitrate must be set", name)
		}
		if p.AudioBitrate == "" {
			t.Errorf("%s: audio bitrate must be set", name)
		}
		if p.MaxFrameRate == 0 {
			t.Errorf("%s: max frame rate must be set", name)
		}
	}
}

func TestDefaultProfile(t *testing.T) {
	p := stream.DefaultProfile()
	if p.Name != "720p" {
		t.Errorf("expected default profile '720p', got %q", p.Name)
	}
}

func TestProfileNames_ContainsOriginal(t *testing.T) {
	names := stream.ProfileNames()
	found := false
	for _, n := range names {
		if n == "original" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ProfileNames() should include 'original'")
	}
}
