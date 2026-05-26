package stream

import (
	"testing"

	librarymodel "hubplay/internal/library/model"
)

func TestDecide_DirectPlay_MP4_H264_AAC(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "mov,mp4,m4a,3gp,3g2,mj2"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectPlay {
		t.Errorf("expected DirectPlay, got %s", d.Method)
	}
}

func TestDecide_DirectStream_MKV_H264_AAC(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska,webm"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "webm"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "vp9", IsDefault: true},
		{StreamType: "audio", Codec: "opus", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectPlay {
		t.Errorf("expected DirectPlay, got %s", d.Method)
	}
}

func TestDecide_RequestedProfile(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	item := &librarymodel.Item{Container: "mp4"}
	d := Decide(item, nil, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode for no streams, got %s", d.Method)
	}
}

func TestDecide_AudioOnly(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "mp4"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	// No video stream → falls back to transcode
	d := Decide(item, streams, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode for audio-only, got %s", d.Method)
	}
}

func TestDecideForceDirectPlay_BypassesCapsForHEVC(t *testing.T) {
	t.Parallel()
	// Daredevil-shaped rip: HEVC video + EAC3 audio + MKV container.
	// Decide() forces a Transcode against web defaults (no HEVC, no
	// EAC3, no MKV). DecideForceDirectPlay must skip the waterfall
	// and return DirectPlay with the file's actual codecs in the
	// response — that's what the player pill renders.
	item := &librarymodel.Item{Container: "matroska,webm"}
	streams := []*librarymodel.MediaStream{
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
	t.Parallel()
	// Multi-language rip: the file has a non-default English audio
	// AND a default Spanish one. DecideForceDirectPlay must pick the
	// flagged default — same convention as Decide() so the player
	// pill labels the dub the user actually hears, not whichever
	// stream happened to be first in the container.
	item := &librarymodel.Item{Container: "matroska,webm"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", Language: "eng"},        // first, non-default
		{StreamType: "audio", Codec: "eac3", Language: "spa", IsDefault: true},
	}
	d := DecideForceDirectPlay(item, streams)
	if d.AudioCodec != "eac3" {
		t.Errorf("AudioCodec = %q, want eac3 (the IsDefault stream)", d.AudioCodec)
	}
}

// HDR HEVC source against the default web client (which doesn't
// declare hdr=...) must Transcode + ToneMap, even though HEVC alone
// would be allowed via DirectStream when the codec is in caps. Pin
// the rule so a future "let HEVC ride DirectStream for everyone"
// refactor doesn't silently send PQ luma to an SDR browser and
// produce the washed-out grey picture this fix exists to prevent.
func TestDecide_HDR_TonemapsForDefaultWebClient(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		hdrType string
	}{
		{"HDR10", "HDR10"},
		{"HLG", "HLG"},
		{"DolbyVision", "DolbyVision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &librarymodel.Item{Container: "matroska"}
			streams := []*librarymodel.MediaStream{
				{StreamType: "video", Codec: "h264", IsDefault: true, HDRType: tc.hdrType},
				{StreamType: "audio", Codec: "aac", IsDefault: true},
			}
			d := Decide(item, streams, nil, "")
			if d.Method != MethodTranscode {
				t.Fatalf("Method = %s, want Transcode (HDR source + SDR client)", d.Method)
			}
			if !d.ToneMap {
				t.Error("ToneMap = false, want true so BuildFFmpegArgs adds the zscale chain")
			}
			if d.CopyVideo {
				t.Error("CopyVideo = true, want false (tonemapping requires a decoded frame)")
			}
		})
	}
}

// Same HDR file, but the client opted in to the matching HDR format
// in the wire header. Now there's nothing to fix up: the source's
// codec / container is also compatible (h264/mkv → DirectStream
// path), so the decision should ride DirectStream with CopyVideo=true
// and ToneMap=false. This is the "native HDR-capable Android TV app"
// scenario.
func TestDecide_HDR_DirectStreamsWhenClientDeclaresHDR(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true, HDRType: "HDR10"},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	caps := &Capabilities{HDRFormats: map[string]bool{"hdr10": true}}
	d := Decide(item, streams, caps, "")
	if d.Method != MethodDirectStream {
		t.Fatalf("Method = %s, want DirectStream (HDR client matches HDR source)", d.Method)
	}
	if !d.CopyVideo {
		t.Error("CopyVideo = false, want true (no need to re-encode)")
	}
	if d.ToneMap {
		t.Error("ToneMap = true, want false (client can render HDR)")
	}
}

// DolbyVision source against a client that declared "dolbyvision"
// (the longer alias) — same outcome as the "dovi" short form. The
// alias matters because the wire header is informal and a
// hand-rolled client could send either.
func TestDecide_HDR_DolbyVisionLongAlias(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true, HDRType: "DolbyVision"},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	caps := &Capabilities{HDRFormats: map[string]bool{"dolbyvision": true}}
	d := Decide(item, streams, caps, "")
	if d.Method == MethodTranscode || d.ToneMap {
		t.Errorf("DolbyVision client (long alias) should not tonemap; got Method=%s ToneMap=%v", d.Method, d.ToneMap)
	}
}

// HDR source where the codec is also incompatible (HEVC). The decision
// should still tonemap — it's the full transcode path either way, but
// without ToneMap=true the encoder would produce washed-out SDR-sized
// frames from HDR-coded source data.
func TestDecide_HDR_HEVCAlsoTonemaps(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true, HDRType: "HDR10"},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	d := Decide(item, streams, nil, "")
	if d.Method != MethodTranscode {
		t.Fatalf("Method = %s, want Transcode", d.Method)
	}
	if !d.ToneMap {
		t.Error("ToneMap = false, want true (HDR HEVC for SDR client must both transcode AND tonemap)")
	}
}

// SDR sources never tonemap regardless of what the client declared
// — `hdr=` is a capability, not a request. Pin so a future bug that
// flips ToneMap unconditionally for any client without hdr=... can't
// re-encode every SDR stream the project serves.
func TestDecide_SDR_NeverTonemaps(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true}, // HDRType deliberately empty
		{StreamType: "audio", Codec: "dts", IsDefault: true},
	}
	d := Decide(item, streams, nil, "")
	if d.ToneMap {
		t.Error("ToneMap = true on SDR source, want false")
	}
}

func TestDecideForceDirectPlay_AudioOnlyItemEmptyVideoCodec(t *testing.T) {
	t.Parallel()
	// Defensive: a row with no video stream returns DirectPlay with
	// an empty VideoCodec rather than panicking. The browser will
	// likely fail to play it, but that's the operator's risk
	// (force_direct_play is opt-in for a reason).
	item := &librarymodel.Item{Container: "mp4"}
	streams := []*librarymodel.MediaStream{
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
