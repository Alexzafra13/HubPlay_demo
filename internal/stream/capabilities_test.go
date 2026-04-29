package stream

import (
	"net/http/httptest"
	"testing"

	"hubplay/internal/db"
)

func TestParseCapabilitiesHeader_Empty(t *testing.T) {
	if got := ParseCapabilitiesHeader(""); got != nil {
		t.Errorf("empty header should return nil, got %+v", got)
	}
}

func TestParseCapabilitiesHeader_NoRecognisedKeys(t *testing.T) {
	// A client that sends only forward-compat keys (e.g. an unknown
	// "subtitle=srt" we haven't shipped support for yet) should be
	// treated the same as if it sent nothing — the decision falls back
	// to defaults.
	if got := ParseCapabilitiesHeader("foo=bar; baz=qux"); got != nil {
		t.Errorf("unknown-keys-only header should yield nil, got %+v", got)
	}
}

func TestParseCapabilitiesHeader_HappyPath(t *testing.T) {
	got := ParseCapabilitiesHeader("video=h264,h265,av1; audio=aac,eac3; container=mp4,mkv")
	if got == nil {
		t.Fatal("expected non-nil capabilities")
	}
	wantVideo := []string{"h264", "h265", "av1"}
	for _, c := range wantVideo {
		if !got.VideoCodecs[c] {
			t.Errorf("video codec %q missing", c)
		}
	}
	if !got.AudioCodecs["eac3"] {
		t.Errorf("audio codec eac3 missing")
	}
	if !got.Containers["mkv"] {
		t.Errorf("container mkv missing")
	}
}

func TestParseCapabilitiesHeader_ToleratesWhitespaceAndCase(t *testing.T) {
	// Real clients format the header in different ways; the parser
	// shouldn't be picky about case or whitespace around tokens. The
	// decision side reads codecs lowercased.
	got := ParseCapabilitiesHeader("  Video = H264 , HEVC ;Audio= AAC  ;container=MP4")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !got.VideoCodecs["h264"] || !got.VideoCodecs["hevc"] {
		t.Errorf("case-insensitive parsing failed: %+v", got.VideoCodecs)
	}
	if !got.AudioCodecs["aac"] {
		t.Errorf("audio aac not parsed")
	}
	if !got.Containers["mp4"] {
		t.Errorf("container mp4 not parsed")
	}
}

func TestParseCapabilitiesHeader_DropsMalformedSegments(t *testing.T) {
	// A typo or spurious token in the middle of an otherwise-valid
	// header should not poison the rest of the parse — the malformed
	// segment is silently dropped and the rest still resolves.
	got := ParseCapabilitiesHeader("video=h264; nokeyhere; audio=aac")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !got.VideoCodecs["h264"] || !got.AudioCodecs["aac"] {
		t.Errorf("valid segments dropped due to malformed neighbour: %+v", got)
	}
}

func TestCapabilitiesFromRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderCapabilities, "video=hevc; audio=truehd; container=mkv")
	caps := CapabilitiesFromRequest(req)
	if caps == nil {
		t.Fatal("expected non-nil")
	}
	if !caps.VideoCodecs["hevc"] {
		t.Errorf("hevc missing — header parsing on request failed")
	}
}

// effectiveCapabilities backfills missing buckets from the web default
// so a partial declaration ("video only") still gets sane audio +
// container checks instead of failing every comparison.
func TestEffectiveCapabilities_BackfillsMissingBuckets(t *testing.T) {
	partial := &Capabilities{
		VideoCodecs: map[string]bool{"hevc": true},
		// AudioCodecs and Containers left nil
	}
	eff := effectiveCapabilities(partial)
	if !eff.VideoCodecs["hevc"] {
		t.Errorf("declared codec missing")
	}
	if !eff.AudioCodecs["aac"] {
		t.Errorf("audio backfill from web defaults missing")
	}
	if !eff.Containers["mp4"] {
		t.Errorf("container backfill from web defaults missing")
	}
}

// Without caps, behaviour matches the legacy hard-coded web defaults.
// This is the regression test for "current web client must keep working
// when it doesn't send the header yet".
func TestDecide_NilCaps_LegacyWebDefaults(t *testing.T) {
	item := &db.Item{Container: "mp4"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	d := Decide(item, streams, nil, "")
	if d.Method != MethodDirectPlay {
		t.Errorf("h264+aac+mp4 should DirectPlay under defaults: got %s", d.Method)
	}

	// HEVC + EAC3 in MKV transcoded under defaults (web doesn't decode
	// either codec natively). This is the case the new caps unlock.
	item2 := &db.Item{Container: "matroska"}
	streams2 := []*db.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true},
		{StreamType: "audio", Codec: "eac3", IsDefault: true},
	}
	d2 := Decide(item2, streams2, nil, "")
	if d2.Method != MethodTranscode {
		t.Errorf("hevc+eac3+mkv should Transcode under defaults: got %s", d2.Method)
	}
}

// The capability win: a client declaring HEVC + EAC3 + MKV gets
// DirectPlay on a file the web defaults forced to Transcode.
func TestDecide_DeclaredCaps_UnlockDirectPlay(t *testing.T) {
	caps := &Capabilities{
		VideoCodecs: map[string]bool{"hevc": true, "h264": true},
		AudioCodecs: map[string]bool{"eac3": true, "aac": true},
		Containers:  map[string]bool{"matroska": true, "mkv": true, "mp4": true},
	}
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "hevc", IsDefault: true},
		{StreamType: "audio", Codec: "eac3", IsDefault: true},
	}
	d := Decide(item, streams, caps, "")
	if d.Method != MethodDirectPlay {
		t.Fatalf("declared HEVC+EAC3+MKV should DirectPlay: got %s container=%s", d.Method, d.Container)
	}
	if d.VideoCodec != "hevc" {
		t.Errorf("VideoCodec passthrough: got %q", d.VideoCodec)
	}
}

// When the client declares the right codecs but a container it can't
// play, we still DirectStream (remux) — same as the old behaviour for
// MKV → MP4 over the web client.
func TestDecide_DeclaredCaps_RemuxToCompatibleContainer(t *testing.T) {
	caps := &Capabilities{
		VideoCodecs: map[string]bool{"h264": true},
		AudioCodecs: map[string]bool{"aac": true},
		Containers:  map[string]bool{"mp4": true}, // client cannot play matroska
	}
	item := &db.Item{Container: "matroska"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	d := Decide(item, streams, caps, "")
	if d.Method != MethodDirectStream {
		t.Fatalf("h264+aac+mkv with mp4-only client should DirectStream: got %s", d.Method)
	}
	if d.Container != "mp4" {
		t.Errorf("DirectStream output container: got %q want mp4", d.Container)
	}
}

// Codec the client doesn't declare → Transcode, regardless of how many
// other capabilities it sends. The waterfall stops short.
func TestDecide_DeclaredCaps_TranscodeOnUnsupportedCodec(t *testing.T) {
	// Client says "I do hevc" but the file is av1; client doesn't
	// declare av1 so we fall through to transcode.
	caps := &Capabilities{
		VideoCodecs: map[string]bool{"hevc": true},
		AudioCodecs: map[string]bool{"aac": true},
		Containers:  map[string]bool{"mp4": true},
	}
	item := &db.Item{Container: "mp4"}
	streams := []*db.MediaStream{
		{StreamType: "video", Codec: "av1", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	d := Decide(item, streams, caps, "")
	if d.Method != MethodTranscode {
		t.Errorf("undeclared av1 should Transcode: got %s", d.Method)
	}
}
