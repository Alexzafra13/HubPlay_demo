package federation

import (
	"strings"
	"testing"
)

// TestRewritePeerMasterPlaylist_AbsoluteURL pins the canonical case:
// origin emits absolute URLs at its own base; we rewrite to our local
// base + local proxy path, preserving the session id.
func TestRewritePeerMasterPlaylist_AbsoluteURL(t *testing.T) {
	body := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,FRAME-RATE=30,NAME="1080p"`,
		"https://pedro.example.com/api/v1/peer/stream/session/abc123/1080p/index.m3u8",
		`#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720,FRAME-RATE=30,NAME="720p"`,
		"https://pedro.example.com/api/v1/peer/stream/session/abc123/720p/index.m3u8",
	}, "\n")

	got := RewritePeerMasterPlaylist(body, "peer-uuid-x", "https://my.local.example")

	wantContains := []string{
		"#EXTM3U",
		"https://my.local.example/api/v1/me/peers/peer-uuid-x/stream/session/abc123/1080p/index.m3u8",
		"https://my.local.example/api/v1/me/peers/peer-uuid-x/stream/session/abc123/720p/index.m3u8",
	}
	for _, w := range wantContains {
		if !strings.Contains(got, w) {
			t.Errorf("rewritten playlist missing %q\nGot:\n%s", w, got)
		}
	}
	// Origin URL should be gone.
	if strings.Contains(got, "pedro.example.com") {
		t.Errorf("rewritten playlist still references origin host:\n%s", got)
	}
}

// TestRewritePeerMasterPlaylist_PreservesComments pins that the
// EXT-X-* directive lines (everything starting with #) pass through
// untouched. A subtle bug here breaks bandwidth/resolution metadata
// the player relies on for variant selection.
func TestRewritePeerMasterPlaylist_PreservesComments(t *testing.T) {
	body := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,FRAME-RATE=30,NAME="1080p"
https://origin.example/api/v1/peer/stream/session/abc/1080p/index.m3u8
`
	got := RewritePeerMasterPlaylist(body, "peer-x", "https://local.example")

	for _, line := range []string{"#EXTM3U", "#EXT-X-VERSION:3", `#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,FRAME-RATE=30,NAME="1080p"`} {
		if !strings.Contains(got, line) {
			t.Errorf("comment/directive missing: %q", line)
		}
	}
}

// TestRewritePeerMasterPlaylist_RelativeURL pins behaviour when the
// origin emits a relative URL (path without scheme/host). We rewrite
// the path prefix; no host substitution happens because there's no
// host to replace. The player resolves relative URLs against the
// playlist's own URL — which is OUR proxy URL — so the result still
// routes through us.
func TestRewritePeerMasterPlaylist_RelativeURL(t *testing.T) {
	body := "#EXTM3U\n/api/v1/peer/stream/session/abc/720p/index.m3u8\n"
	got := RewritePeerMasterPlaylist(body, "peer-x", "https://local.example")

	want := "/api/v1/me/peers/peer-x/stream/session/abc/720p/index.m3u8"
	if !strings.Contains(got, want) {
		t.Errorf("relative URL rewrite missing %q\nGot:\n%s", want, got)
	}
}

// TestRewritePeerMasterPlaylist_NoTrailingNewlineWhenAbsent: input
// without a trailing newline shouldn't get one added. Some HLS clients
// are forgiving but byte-for-byte compatibility is the safer bet.
func TestRewritePeerMasterPlaylist_NoTrailingNewlineWhenAbsent(t *testing.T) {
	body := "#EXTM3U\nhttps://x.example/api/v1/peer/stream/session/s/1080p/index.m3u8"
	got := RewritePeerMasterPlaylist(body, "peer-x", "https://local")
	if strings.HasSuffix(got, "\n") {
		t.Errorf("rewrite added trailing newline that wasn't there\nGot: %q", got)
	}
}
