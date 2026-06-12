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

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "", -1)
	if d.Method != MethodDirectStream {
		t.Fatalf("h264 + AC3 in 'matroska,webm' must DirectStream (video copy), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true even when container is comma-list")
	}
}

// PB-1 (audit 2026-06-10): ffprobe etiqueta TODO Matroska como
// "matroska,webm" (comparten demuxer). Un MKV h264+aac — el fichero más
// común del mundo real — matcheaba el "webm" de las caps del cliente y
// se servía como DirectPlay con Content-Type matroska: Chrome suele
// tragarlo, Firefox y Safari dan pantalla negra. El path correcto es
// DirectStream (remux). El alias webm solo cuenta si los códecs caben
// en un WebM real (vp8/vp9/av1 + opus/vorbis).
func TestDecide_MKV_WebmAlias_H264_AAC_MustNotDirectPlay(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska,webm"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", -1)
	if d.Method != MethodDirectStream {
		t.Fatalf("h264 + AAC in 'matroska,webm' must DirectStream (remux), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true (h264 is client-compatible)")
	}
	if !d.CopyAudio {
		t.Error("expected CopyAudio=true (aac is client-compatible)")
	}
}

// Un WebM de verdad también llega como "matroska,webm" desde ffprobe —
// con códecs WebM-legales el alias sí cuenta y DirectPlay se mantiene.
func TestDecide_DirectPlay_WebmAlias_VP9_Opus(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska,webm"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "vp9", IsDefault: true},
		{StreamType: "audio", Codec: "opus", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", -1)
	if d.Method != MethodDirectPlay {
		t.Errorf("real webm (vp9+opus) reported as 'matroska,webm' must DirectPlay, got %s", d.Method)
	}
}

func TestDecide_DirectPlay_WebM_VP9_Opus(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "webm"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "vp9", IsDefault: true},
		{StreamType: "audio", Codec: "opus", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", -1)
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

	d := Decide(item, streams, nil, "480p", -1)
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
	d := Decide(item, nil, nil, "", -1)
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
	d := Decide(item, streams, nil, "", -1)
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
			d := Decide(item, streams, nil, "", -1)
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
	d := Decide(item, streams, caps, "", -1)
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
	d := Decide(item, streams, caps, "", -1)
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
	d := Decide(item, streams, nil, "", -1)
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
	d := Decide(item, streams, nil, "", -1)
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

// ─── PB-6: la decisión debe evaluar la pista de audio SELECCIONADA ───

// MKV con default AAC + pista DTS: al seleccionar la DTS (índice 1) la
// decisión debe re-encodear el audio. Antes evaluaba solo la default →
// DirectStream con CopyAudio=true → DTS copiado al TS → vídeo mudo.
func TestDecide_SelectedAudioTrack_DTS_ForcesAudioReencode(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
		{StreamType: "audio", Codec: "dts"},
	}

	d := Decide(item, streams, nil, "", 1)
	if d.Method != MethodDirectStream {
		t.Fatalf("expected DirectStream, got %s", d.Method)
	}
	if d.CopyAudio {
		t.Error("expected CopyAudio=false: la pista seleccionada es DTS, no la default AAC")
	}
	if d.AudioCodec != "dts" {
		t.Errorf("expected AudioCodec=dts (la pista seleccionada), got %q", d.AudioCodec)
	}
}

// Caso inverso: default DTS + selección de la pista AAC → el audio se
// puede copiar (antes re-encodeaba sin necesidad).
func TestDecide_SelectedAudioTrack_AAC_AllowsAudioCopy(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "dts", IsDefault: true},
		{StreamType: "audio", Codec: "aac"},
	}

	d := Decide(item, streams, nil, "", 1)
	if d.Method != MethodDirectStream {
		t.Fatalf("expected DirectStream, got %s", d.Method)
	}
	if !d.CopyAudio {
		t.Error("expected CopyAudio=true: la pista seleccionada es AAC compatible")
	}
}

// Índice fuera de rango → fallback a la default (la validación dura
// vive en el Manager; Decide es robusto por sí mismo).
func TestDecide_AudioIndexOutOfRange_FallsBackToDefault(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", 7)
	if d.AudioCodec != "aac" {
		t.Errorf("expected fallback to default aac, got %q", d.AudioCodec)
	}
}

// ─── PB-7: mp4/mov son remuxeables ───

// MP4 h264+AC3 (rip típico): solo el audio es incompatible. Antes caía
// a re-encode completo del vídeo porque mp4 no estaba en
// remuxableContainers; basta DirectStream con -c:v copy.
func TestDecide_DirectStream_MP4_H264_AC3(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "mov,mp4,m4a,3gp,3g2,mj2"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", -1)
	if d.Method != MethodDirectStream {
		t.Fatalf("mp4 h264+ac3 must DirectStream (remux + audio reencode), got %s", d.Method)
	}
	if !d.CopyVideo {
		t.Error("expected CopyVideo=true")
	}
	if d.CopyAudio {
		t.Error("expected CopyAudio=false (ac3)")
	}
}

// ─── PB-8: perfiles h264 que ningún navegador decodifica ───

// h264 High 10 (Hi10P, anime): el nombre del codec matchea las caps
// pero NINGÚN navegador lo decodifica. Debe forzar Transcode, nunca
// DirectPlay/DirectStream (copiar Hi10P al TS tampoco ayuda).
func TestDecide_H264High10_ForcesTranscode(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", Profile: "High 10", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	d := Decide(item, streams, nil, "", -1)
	if d.Method != MethodTranscode {
		t.Fatalf("h264 High 10 must Transcode, got %s", d.Method)
	}
	if d.CopyVideo {
		t.Error("expected CopyVideo=false (Hi10P necesita re-encode)")
	}
}

// HEVC Main 10 NO se gatea: todo decoder HW de HEVC soporta Main 10 —
// un cliente que declara hevc debe seguir haciendo direct play.
func TestDecide_HEVCMain10_NotGatedWhenCapsDeclareHEVC(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "mp4"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "hevc", Profile: "Main 10", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}
	caps := &Capabilities{
		VideoCodecs: map[string]bool{"hevc": true, "h264": true},
		AudioCodecs: map[string]bool{"aac": true},
		Containers:  map[string]bool{"mp4": true},
	}

	d := Decide(item, streams, caps, "", -1)
	if d.Method != MethodDirectPlay {
		t.Errorf("hevc Main 10 with hevc-capable client must DirectPlay, got %s", d.Method)
	}
}

// ─── PB-22: canales de audio del transcode ───────────────────────────

// Fuente 5.1 AC3 + cliente que declara channels=6: el transcode de
// audio debe conservar el surround (-ac 6) en vez del downmix a
// estéreo incondicional histórico.
func TestDecide_AudioChannels_SurroundPreservedWhenClientDeclares(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "ac3", Channels: 6, IsDefault: true},
	}
	caps := ParseCapabilitiesHeader("video=h264; audio=aac; container=mp4; channels=6")

	d := Decide(item, streams, caps, "", -1)
	if d.Method != MethodDirectStream || d.CopyAudio {
		t.Fatalf("expected DirectStream with audio reencode, got %s CopyAudio=%v", d.Method, d.CopyAudio)
	}
	if d.AudioChannels != 6 {
		t.Errorf("AudioChannels = %d, want 6", d.AudioChannels)
	}
}

func TestDecide_AudioChannels_Matrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		srcChannels int
		header      string
		want        int
	}{
		{"cliente sin channels= → estéreo", 6, "video=h264; audio=aac", 2},
		{"7.1 se pliega a 5.1", 8, "video=h264; audio=aac; channels=8", 6},
		{"fuente estéreo no se infla", 2, "video=h264; audio=aac; channels=6", 2},
		{"fuente sin metadata de canales → estéreo", 0, "video=h264; audio=aac; channels=6", 2},
		{"mono se respeta", 1, "video=h264; audio=aac; channels=6", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := &librarymodel.Item{Container: "matroska"}
			streams := []*librarymodel.MediaStream{
				{StreamType: "video", Codec: "h264", IsDefault: true},
				{StreamType: "audio", Codec: "dts", Channels: tc.srcChannels, IsDefault: true},
			}
			d := Decide(item, streams, ParseCapabilitiesHeader(tc.header), "", -1)
			if d.AudioChannels != tc.want {
				t.Errorf("AudioChannels = %d, want %d", d.AudioChannels, tc.want)
			}
		})
	}
}

// El default web (sin header) mantiene el comportamiento histórico:
// downmix a estéreo.
func TestDecide_AudioChannels_NilCapsDefaultsToStereo(t *testing.T) {
	t.Parallel()
	item := &librarymodel.Item{Container: "matroska"}
	streams := []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "dts", Channels: 6, IsDefault: true},
	}
	d := Decide(item, streams, nil, "", -1)
	if d.AudioChannels != 2 {
		t.Errorf("AudioChannels = %d, want 2 (default web caps)", d.AudioChannels)
	}
}
