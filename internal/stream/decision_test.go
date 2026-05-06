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
