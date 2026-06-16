package torrentstream

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildVODRemuxArgs_AudioCopy(t *testing.T) {
	args := buildVODRemuxArgs("http://127.0.0.1:9/abc", "/tmp/work", false)
	joined := strings.Join(args, " ")

	if !slices.Contains(args, "-i") || !slices.Contains(args, "http://127.0.0.1:9/abc") {
		t.Fatalf("input url missing: %v", args)
	}
	// Video always copied (the whole point — near-zero CPU).
	if !strings.Contains(joined, "-c:v copy") {
		t.Errorf("video must be copied: %s", joined)
	}
	// AAC source → audio copied too.
	if !strings.Contains(joined, "-c:a copy") {
		t.Errorf("aac audio should be copied: %s", joined)
	}
	if strings.Contains(joined, "-c:a aac") {
		t.Errorf("should not re-encode aac audio: %s", joined)
	}
	if !strings.HasSuffix(joined, "index.m3u8") {
		t.Errorf("output should be the hls playlist: %s", joined)
	}
}

func TestBuildVODRemuxArgs_AudioTranscode(t *testing.T) {
	args := buildVODRemuxArgs("http://127.0.0.1:9/abc", "/tmp/work", true)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v copy") {
		t.Errorf("video must still be copied even when audio is re-encoded: %s", joined)
	}
	if !strings.Contains(joined, "-c:a aac") {
		t.Errorf("non-aac audio should be re-encoded to aac: %s", joined)
	}
}

func TestBuildVODReencodeArgs(t *testing.T) {
	// Software path.
	sw := strings.Join(buildVODReencodeArgs("http://127.0.0.1:9/x", "/tmp/w", "", nil), " ")
	if !strings.Contains(sw, "-c:v libx264") {
		t.Errorf("empty encoder should default to libx264: %s", sw)
	}
	if !strings.Contains(sw, "-c:a aac") {
		t.Errorf("reencode should output aac audio: %s", sw)
	}

	// Hardware path: VAAPI needs the decode-side flags BEFORE -i and a
	// hwupload filter before the encoder.
	hw := buildVODReencodeArgs("http://127.0.0.1:9/x", "/tmp/w", "h264_vaapi", []string{"-hwaccel", "vaapi"})
	joined := strings.Join(hw, " ")
	if !strings.Contains(joined, "-c:v h264_vaapi") {
		t.Errorf("encoder not applied: %s", joined)
	}
	if !strings.Contains(joined, "format=nv12,hwupload") {
		t.Errorf("vaapi should upload frames to the GPU: %s", joined)
	}
	iHwaccel := indexOf(hw, "-hwaccel")
	iInput := indexOf(hw, "-i")
	if iHwaccel < 0 || iInput < 0 || iHwaccel > iInput {
		t.Errorf("-hwaccel must precede -i: hwaccel@%d input@%d", iHwaccel, iInput)
	}
}

func indexOf(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}

func TestVODHLSOutputArgs_VODSeekable(t *testing.T) {
	joined := strings.Join(vodHLSOutputArgs("/tmp/work"), " ")
	// Event playlist + list_size 0 keep all segments so the title is seekable.
	if !strings.Contains(joined, "-hls_playlist_type event") {
		t.Errorf("expected event playlist: %s", joined)
	}
	if !strings.Contains(joined, "-hls_list_size 0") {
		t.Errorf("expected list_size 0 (keep all segments): %s", joined)
	}
	if !strings.Contains(joined, "temp_file") {
		t.Errorf("expected atomic temp_file segment writes: %s", joined)
	}
}
