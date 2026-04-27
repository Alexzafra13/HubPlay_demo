package iptv

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestIsVODChannel(t *testing.T) {
	cases := []struct {
		name string
		ch   M3UChannel
		want bool
	}{
		{
			name: "live channel — no group, plain URL",
			ch:   M3UChannel{Name: "Telecinco HD", StreamURL: "https://live.example.com/cinco/master.m3u8"},
			want: false,
		},
		{
			name: "Xtream movie URL is VOD",
			ch:   M3UChannel{Name: "Inception", StreamURL: "http://provider.com/movie/user/pass/12345.mp4"},
			want: true,
		},
		{
			name: "Xtream series URL is VOD",
			ch:   M3UChannel{Name: "Breaking Bad S01E01", StreamURL: "http://provider.com/series/user/pass/9876.mkv"},
			want: true,
		},
		{
			name: "group-title VOD",
			ch:   M3UChannel{Name: "Some Movie", GroupName: "VOD | Action", StreamURL: "http://x.com/foo.ts"},
			want: true,
		},
		{
			name: "group-title PELICULAS spanish",
			ch:   M3UChannel{Name: "Foo", GroupName: "PELÍCULAS LATINAS", StreamURL: "http://x.com/foo.ts"},
			want: true,
		},
		{
			name: "group-title series",
			ch:   M3UChannel{Name: "Foo", GroupName: "Series HD", StreamURL: "http://x.com/foo.ts"},
			want: true,
		},
		{
			name: "group-title Adultos is VOD",
			ch:   M3UChannel{Name: "Foo", GroupName: "Adultos 18+", StreamURL: "http://x.com/foo.ts"},
			want: true,
		},
		{
			name: "live channel with sport group survives",
			ch:   M3UChannel{Name: "DAZN F1", GroupName: "Deportes", StreamURL: "http://x.com/dazn-f1/index.m3u8"},
			want: false,
		},
		{
			name: "live channel with news group survives",
			ch:   M3UChannel{Name: "CNN", GroupName: "News HD", StreamURL: "http://x.com/cnn/master.m3u8"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsVODChannel(tc.ch); got != tc.want {
				t.Fatalf("IsVODChannel(%q, group=%q) = %v, want %v",
					tc.ch.StreamURL, tc.ch.GroupName, got, tc.want)
			}
		})
	}
}

func TestParseM3UStream_EmitsAllChannels(t *testing.T) {
	src := `#EXTM3U url-tvg="https://x/epg.xml"
#EXTINF:-1 tvg-id="ch1" group-title="News",CNN
http://x.com/cnn.m3u8
#EXTINF:-1 tvg-id="ch2" group-title="Movies",Inception
http://x.com/movie/u/p/123.mp4
#EXTINF:-1 tvg-id="ch3" group-title="Sports",DAZN
http://x.com/dazn.m3u8
`
	var got []M3UChannel
	epg, lines, err := ParseM3UStream(strings.NewReader(src), func(ch M3UChannel) error {
		got = append(got, ch)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if epg != "https://x/epg.xml" {
		t.Errorf("epg url = %q, want https://x/epg.xml", epg)
	}
	if len(got) != 3 {
		t.Fatalf("emitted %d channels, want 3", len(got))
	}
	if lines == 0 {
		t.Errorf("lineNum = 0, expected > 0")
	}
	if got[1].StreamURL != "http://x.com/movie/u/p/123.mp4" {
		t.Errorf("got[1].StreamURL = %q", got[1].StreamURL)
	}
}

func TestParseM3UStream_CallbackErrorAborts(t *testing.T) {
	src := `#EXTM3U
#EXTINF:-1,A
http://x/a.m3u8
#EXTINF:-1,B
http://x/b.m3u8
`
	stop := errors.New("stop")
	count := 0
	_, _, err := ParseM3UStream(strings.NewReader(src), func(M3UChannel) error {
		count++
		return stop
	})
	if err == nil || !errors.Is(err, stop) {
		t.Fatalf("err = %v, want wrapped stop sentinel", err)
	}
	if count != 1 {
		t.Errorf("callback ran %d times, want 1 (should abort on first error)", count)
	}
}

// failingReader returns n bytes of valid M3U content and then a hard
// io.ErrUnexpectedEOF, simulating a server that drops the connection
// mid-playlist (the failure mode that motivated the streaming refactor).
type failingReader struct {
	body []byte
	pos  int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.body) {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, r.body[r.pos:])
	r.pos += n
	return n, nil
}

func TestParseM3UStream_RejectsHTMLAsErrNotM3U(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "doctype html",
			body: `<!DOCTYPE html>
<html lang="en">
<head><title>Blocked</title></head>
<body><p>IP blocked by court order</p></body></html>`,
		},
		{
			name: "leading whitespace then html tag",
			body: "\n\n  <html><body>nope</body></html>",
		},
		{
			name: "soap-style xml fault",
			body: `<?xml version="1.0"?><error>account suspended</error>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseM3UStream(strings.NewReader(tc.body), func(M3UChannel) error {
				t.Fatal("callback must not be invoked when body is not M3U")
				return nil
			})
			if !errors.Is(err, ErrNotM3U) {
				t.Fatalf("err = %v, want ErrNotM3U", err)
			}
		})
	}
}

func TestParseM3UStream_AcceptsValidM3UWithoutHeader(t *testing.T) {
	// The parser is lenient about #EXTM3U being present; ErrNotM3U
	// must only fire on `<` first-byte, not on any non-#EXTM3U input.
	src := `#EXTINF:-1,Solo
http://x/solo.m3u8
`
	calls := 0
	_, _, err := ParseM3UStream(strings.NewReader(src), func(M3UChannel) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 {
		t.Errorf("emitted %d, want 1", calls)
	}
}

func TestParseM3UStream_TruncatedSourceReturnsErrAndPartialChannels(t *testing.T) {
	// 3 channels worth of body, then unexpected EOF mid-stream.
	src := `#EXTM3U
#EXTINF:-1 tvg-id="a",A
http://x/a.m3u8
#EXTINF:-1 tvg-id="b",B
http://x/b.m3u8
#EXTINF:-1 tvg-id="c",C
http://x/c.m3u8
`
	r := &failingReader{body: []byte(src)}

	var got []M3UChannel
	_, _, err := ParseM3UStream(r, func(ch M3UChannel) error {
		got = append(got, ch)
		return nil
	})
	if err == nil {
		t.Fatalf("expected error from truncated reader, got nil")
	}
	// We must have emitted at least the channels that were complete
	// before the EOF — that's the behaviour the partial-commit path
	// in RefreshM3U relies on.
	if len(got) < 2 {
		t.Errorf("emitted %d channels before EOF, want >= 2", len(got))
	}
}
