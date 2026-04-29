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
	if d.Container != "mp4" {
		t.Errorf("expected mp4 container, got %s", d.Container)
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

func TestDecide_Transcode_IncompatibleAudio(t *testing.T) {
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "dts", IsDefault: true},
	}

	d := Decide(item, streams, nil, "")
	if d.Method != MethodTranscode {
		t.Errorf("expected Transcode, got %s", d.Method)
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
