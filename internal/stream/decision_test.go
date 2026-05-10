package stream

import (
	"testing"

	"hubplay/internal/db"
)

func TestDecide_DirectPlay_MP4_H264_AAC(t *testing.T) {
	item := &db.Item{Container: "mov,mp4,m4a,3gp,3g2,mj2"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectPlay {
		t.Errorf("expected DirectPlay, got %s", d.Method)
	}
}

func TestDecide_DirectStream_MKV_H264_AAC(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectStream {
		t.Errorf("expected DirectStream, got %s", d.Method)
	}
	// Both streams compatible — copy them through, no re-encode at all.
	if !d.CopyVideo || !d.CopyAudio {
		t.Errorf("expected CopyVideo+CopyAudio both true, got CopyVideo=%v CopyAudio=%v", d.CopyVideo, d.CopyAudio)
	}
}

// h264 video with AC3 / DTS audio in mkv: the BluRay-rip case. Pre-fix
// this hit MethodTranscode and re-encoded the (expensive) video for
// no reason — the video stream is already client-compatible. The fix
// promotes this to DirectStream with CopyVideo=true, CopyAudio=false:
// ffmpeg copies video bytes and only re-encodes the (cheap) audio.
func TestDecide_DirectStream_VideoCopyAudioReencode_AC3(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectStream {
		t.Errorf("expected DirectStream (video copy + audio reencode), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true (video stream is client-compatible)")
	}
	if d.CopyAudio {
		t.Error("expected CopyAudio=false (AC3 not in default web caps, audio must be reencoded)")
	}
}

func TestDecide_Transcode_HEVC(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode, got %s", d.Method)
	}
}

// Mirror of the AC3 test for DTS — same outcome, just a different
// audio codec the browser can't decode natively.
func TestDecide_DirectStream_VideoCopyAudioReencode_DTS(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "dts", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectStream {
		t.Errorf("expected DirectStream (video copy + audio reencode), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true")
	}
	if d.CopyAudio {
		t.Error("expected CopyAudio=false (DTS not supported)")
	}
}

// Real-world ffprobe outputs the format_name field as a comma-
// separated list (e.g. "matroska,webm"). The remuxable-containers
// check has to recognise the file regardless of which label ffprobe
// picked; otherwise every mkv on disk would silently fall to full
// transcode because the literal "matroska,webm" string doesn't
// match the map keys.
func TestDecide_DirectStream_FormatNameCommaList(t *testing.T) {
	item := &db.Item{Container: "matroska,webm"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectStream {
		t.Fatalf("h264 + AC3 in 'matroska,webm' must DirectStream (video copy), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true even when container is comma-list")
	}
}

func TestDecide_DirectPlay_WebM_VP9_Opus(t *testing.T) {
	item := &db.Item{Container: "webm"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "vp9", IsDefault: true},
		{StreamType: "audio", Codec: "opus", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectPlay {
		t.Errorf("expected DirectPlay, got %s", d.Method)
	}
}

func TestDecide_RequestedProfile(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true},
		{StreamType: "audio", Codec: "dts", IsDefault: true},
	}

	d := Decide(item, streams, nil, "480p")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode, got %s", d.Method)
	}
	if d.Profile.Name != "480p" {
		t.Errorf("expected 480p profile, got %s", d.Profile.Name)
	}
}

func TestDecide_NoStreams(t *testing.T) {
	item := &db.Item{Container: "mp4"}
	d := Decide(item, nil, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode for no streams, got %s", d.Method)
	}
}

func TestDecide_AudioOnly(t *testing.T) {
	item := &db.Item{Container: "mp4"}
	streams := []*db.MediaStream{
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	// No video stream → falls back to transcode
	d := Decide(item, streams, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode for audio-only, got %s", d.Method)
	}
}

func TestDecideForceDirectPlay_BypassesCapsForHEVC(t *testing.T) {
	// Daredevil-shaped rip: HEVC video + EAC3 audio + MKV container.
	// Decide() forces a Transcode against web defaults (no HEVC, no
	// EAC3, no MKV). DecideForceDirectPlay must skip the waterfall
	// and return DirectPlay with the file's actual codecs in the
	// response — that's what the player pill renders.
	item := &db.Item{Container: "matroska,webm"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true},
		{StreamType: "audio", Codec: "eac3", IsDefault: true},
	}
	d := DecideForceDirectPlay(item, streams)
	if d.Method != MethodDirectPlay {
		t.Fatalf("Method = %s, want DirectPlay", d.Method)
	}
	if d.VideoCodec != "hevc" {
		t.Errorf("VideoCodec = %q, want hevc (from the file, not the encoder)", d.VideoCodec)
	}
	if d.AudioCodec != "eac3" {
		t.Errorf("AudioCodec = %q, want eac3", d.AudioCodec)
	}
	if d.Container != "matroska,webm" {
		t.Errorf("Container = %q, want the raw ffprobe value", d.Container)
	}
	// DirectPlay never spins up ffmpeg, so the copy flags + profile
	// are zero-value by construction. Pin that so a future "pass
	// the profile through anyway" change is at least deliberate.
	if d.CopyVideo || d.CopyAudio {
		t.Errorf("CopyVideo/CopyAudio should be false for DirectPlay (got %v / %v)", d.CopyVideo, d.CopyAudio)
	}
}

func TestDecideForceDirectPlay_PrefersDefaultStream(t *testing.T) {
	// Multi-language rip: the file has a non-default English audio
	// AND a default Spanish one. DecideForceDirectPlay must pick the
	// flagged default — same convention as Decide() so the player
	// pill labels the dub the user actually hears, not whichever
	// stream happened to be first in the container.
	item := &db.Item{Container: "matroska,webm"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", Language: "eng"},        // first, non-default
		{StreamType: "audio", Codec: "eac3", Language: "spa", IsDefault: true},
	}
	d := DecideForceDirectPlay(item, streams)
	if d.AudioCodec != "eac3" {
		t.Errorf("AudioCodec = %q, want eac3 (the IsDefault stream)", d.AudioCodec)
	}
}

func TestDecideForceDirectPlay_AudioOnlyItemEmptyVideoCodec(t *testing.T) {
	// Defensive: a row with no video stream returns DirectPlay with
	// an empty VideoCodec rather than panicking. The browser will
	// likely fail to play it, but that's the operator's risk
	// (force_direct_play is opt-in for a reason).
	item := &db.Item{Container: "mp4"}
	streams := []*db.MediaStream{
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	d := DecideForceDirectPlay(item, streams)
	if d.Method != MethodDirectPlay {
		t.Errorf("Method = %s, want DirectPlay even on audio-only", d.Method)
	}
	if d.VideoCodec != "" {
		t.Errorf("VideoCodec = %q, want empty", d.VideoCodec)
	}
	if d.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want aac", d.AudioCodec)
	}
}
