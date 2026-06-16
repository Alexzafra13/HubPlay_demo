package torrentstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewTorznabClient(StaticIndexers{{Name: "x", URL: srv.URL + "/torznab"}}, nil)
	_, err := c.Search(context.Background(), Query{Type: MediaTypeMovie, IMDbID: "tt0133093"})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

// After a 429 the endpoint must be skipped for the cooldown window instead of
// being hammered again (the snowball that gets you self-banned).
func TestSearchCircuitBreaker(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewTorznabClient(StaticIndexers{{Name: "x", URL: srv.URL + "/torznab"}}, nil)

	if _, err := c.Search(context.Background(), Query{Type: MediaTypeMovie, IMDbID: "tt0133093"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("first search: want ErrRateLimited, got %v", err)
	}
	after := hits.Load()
	if after == 0 {
		t.Fatal("expected the endpoint to be queried at least once")
	}

	// Second search must skip the cooling-down endpoint (no new hits) yet still
	// surface the rate-limit so the UI can say "wait and retry".
	if _, err := c.Search(context.Background(), Query{Type: MediaTypeMovie, IMDbID: "tt0133093"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second search: want ErrRateLimited, got %v", err)
	}
	if got := hits.Load(); got != after {
		t.Errorf("breaker should skip the endpoint: hits went %d → %d", after, got)
	}
}

// When the precise imdbid pass already returns hits, the broad text pass must
// be skipped (halves the load on the endpoint).
func TestSearchDeAmplifiesWhenIMDbHits(t *testing.T) {
	var imdbHits, textHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "search" {
			textHits.Add(1)
		} else { // t=movie / t=tvsearch → the imdbid pass
			imdbHits.Add(1)
		}
		_, _ = io.WriteString(w, sampleFeed)
	}))
	defer srv.Close()
	c := NewTorznabClient(StaticIndexers{{Name: "x", URL: srv.URL + "/torznab"}}, nil)

	res, err := c.Search(context.Background(), Query{
		Type: MediaTypeMovie, IMDbID: "tt0133093", Title: "Some Movie", Year: 2020,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("expected results from the imdbid pass")
	}
	if got := imdbHits.Load(); got != 1 {
		t.Errorf("imdbid pass should run once, got %d", got)
	}
	if got := textHits.Load(); got != 0 {
		t.Errorf("text pass should be skipped when imdbid hits, got %d", got)
	}
}

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>Some Movie 2020 1080p BluRay x264-GROUP</title>
      <guid>https://idx/details/aaa</guid>
      <link>magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&amp;dn=Some+Movie</link>
      <size>1500000000</size>
      <torznab:attr name="seeders" value="42" />
      <torznab:attr name="peers" value="50" />
    </item>
    <item>
      <title>Some Movie 2020 2160p UHD x265-GROUP</title>
      <guid>https://idx/details/bbb</guid>
      <link>https://idx/download/bbb.torrent</link>
      <enclosure url="https://idx/download/bbb.torrent" length="8000000000" type="application/x-bittorrent" />
      <torznab:attr name="seeders" value="10" />
      <torznab:attr name="infohash" value="BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB" />
      <torznab:attr name="size" value="8000000000" />
    </item>
    <item>
      <title>Junk with no source</title>
      <guid></guid>
    </item>
  </channel>
</rss>`

func TestParseAndNormalize(t *testing.T) {
	items, err := parseTorznab(strings.NewReader(sampleFeed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items: got %d want 3", len(items))
	}

	idx := TorznabIndexer{Name: "test", Trackers: []string{"udp://tr.example:80"}}
	var out []SearchResult
	for _, it := range items {
		if r, ok := normalizeItem(it, idx); ok {
			out = append(out, r)
		}
	}
	if len(out) != 2 {
		t.Fatalf("normalized: got %d want 2 (junk dropped)", len(out))
	}

	// First item: magnet in <link>, infohash extracted from it.
	m := out[0]
	if m.Seeders != 42 {
		t.Errorf("seeders: got %d want 42", m.Seeders)
	}
	if m.Quality != "1080p" {
		t.Errorf("quality: got %q want 1080p", m.Quality)
	}
	if m.SizeBytes != 1500000000 {
		t.Errorf("size: got %d", m.SizeBytes)
	}
	if m.InfoHash != strings.ToLower("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Errorf("infohash: got %q", m.InfoHash)
	}
	if !strings.HasPrefix(m.MagnetURI, "magnet:?xt=urn:btih:") {
		t.Errorf("magnet: got %q", m.MagnetURI)
	}

	// Second item: http .torrent link + infohash attr (no magnet). We
	// build a magnet from the hash + indexer trackers and prefer it as the
	// stream source (plays over the BitTorrent network, no SSRF fetch).
	u := out[1]
	if u.Quality != "4K" {
		t.Errorf("quality: got %q want 4K", u.Quality)
	}
	if u.InfoHash != strings.ToLower("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB") {
		t.Errorf("infohash: got %q", u.InfoHash)
	}
	if !strings.HasPrefix(u.TorrentURL, "magnet:?xt=urn:btih:") {
		t.Errorf("stream src should be the built magnet: %q", u.TorrentURL)
	}
	if !strings.Contains(u.MagnetURI, "tr=udp") {
		t.Errorf("built magnet missing tracker: %q", u.MagnetURI)
	}
}

func TestSortSources(t *testing.T) {
	in := []SearchResult{
		{Title: "a", Seeders: 5, Quality: "720p", SizeBytes: 100},
		{Title: "b", Seeders: 50, Quality: "1080p", SizeBytes: 100},
		{Title: "c", Seeders: 50, Quality: "4K", SizeBytes: 100},
		{Title: "d", Seeders: 50, Quality: "4K", SizeBytes: 999},
	}
	sortSources(in)
	// seeders desc → b/c/d before a; among the 50-seeders, 4K before
	// 1080p; among the two 4K, larger size first.
	got := []string{in[0].Title, in[1].Title, in[2].Title, in[3].Title}
	want := []string{"d", "c", "b", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v want %v", got, want)
		}
	}
}

func TestDedupeByInfoHash(t *testing.T) {
	in := []SearchResult{
		{Title: "best", InfoHash: "abc", Seeders: 50},
		{Title: "dup", InfoHash: "abc", Seeders: 10},
		{Title: "other", InfoHash: "def"},
		{Title: "nohash1", InfoHash: ""},
		{Title: "nohash2", InfoHash: ""},
	}
	out := dedupeByInfoHash(in)
	if len(out) != 4 {
		t.Fatalf("len: got %d want 4", len(out))
	}
	if out[0].Title != "best" {
		t.Errorf("kept wrong dup survivor: %q", out[0].Title)
	}
}

func TestBuildTorznabURL(t *testing.T) {
	idx := TorznabIndexer{Name: "prowlarr"}
	endpoint := "http://localhost:9696/api/torznab"
	raw, err := buildTorznabURL(endpoint, "secret", idx, Query{Type: MediaTypeMovie, IMDbID: "tt0133093"}, modeIMDb)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if q.Get("t") != "movie" {
		t.Errorf("t: %q", q.Get("t"))
	}
	if q.Get("imdbid") != "0133093" {
		t.Errorf("imdbid should be stripped of tt: %q", q.Get("imdbid"))
	}
	if q.Get("cat") != "2000" {
		t.Errorf("cat: %q", q.Get("cat"))
	}
	if q.Get("apikey") != "secret" {
		t.Errorf("apikey: %q", q.Get("apikey"))
	}
	if q.Get("o") != "xml" {
		t.Errorf("o: %q", q.Get("o"))
	}

	// Series → tvsearch + default tv category.
	raw2, _ := buildTorznabURL(endpoint, "secret", idx, Query{Type: MediaTypeSeries, IMDbID: "tt1234567"}, modeIMDb)
	u2, _ := url.Parse(raw2)
	if u2.Query().Get("t") != "tvsearch" {
		t.Errorf("series t: %q", u2.Query().Get("t"))
	}
	if u2.Query().Get("cat") != "5000" {
		t.Errorf("series cat: %q", u2.Query().Get("cat"))
	}
}

func TestBuildTorznabURLTextMode(t *testing.T) {
	idx := TorznabIndexer{Name: "prowlarr"}
	endpoint := "http://localhost:9696/api/torznab"

	// Movie text pass: generic t=search with "Title Year", no imdbid.
	raw, err := buildTorznabURL(endpoint, "", idx,
		Query{Type: MediaTypeMovie, Title: "Dune", Year: 2021, IMDbID: "tt1160419"}, modeText)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	q := mustParseQuery(t, raw)
	if q.Get("t") != "search" {
		t.Errorf("t: got %q want search", q.Get("t"))
	}
	if q.Get("q") != "Dune 2021" {
		t.Errorf("q: got %q want %q", q.Get("q"), "Dune 2021")
	}
	if q.Get("imdbid") != "" {
		t.Errorf("text mode must not send imdbid, got %q", q.Get("imdbid"))
	}

	// Series imdbid pass carries season/ep.
	raw2, _ := buildTorznabURL(endpoint, "", idx,
		Query{Type: MediaTypeSeries, IMDbID: "tt0903747", Season: 1, Episode: 2}, modeIMDb)
	q2 := mustParseQuery(t, raw2)
	if q2.Get("season") != "1" || q2.Get("ep") != "2" {
		t.Errorf("series imdb mode season/ep: season=%q ep=%q", q2.Get("season"), q2.Get("ep"))
	}

	// Series text pass embeds SxxEyy in the query string.
	raw3, _ := buildTorznabURL(endpoint, "", idx,
		Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2}, modeText)
	if got := mustParseQuery(t, raw3).Get("q"); got != "Breaking Bad S01E02" {
		t.Errorf("series text q: got %q", got)
	}
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Query()
}

func TestBuildTorznabURLInvalid(t *testing.T) {
	if _, err := buildTorznabURL("not-a-url", "", TorznabIndexer{}, Query{Type: MediaTypeMovie, IMDbID: "tt1"}, modeIMDb); err == nil {
		t.Fatal("expected error for url without scheme/host")
	}
}

func TestExtractQuality(t *testing.T) {
	cases := map[string]string{
		"Movie 2160p":      "4K",
		"Movie 4K HDR":     "4K",
		"Movie 1080p":      "1080p",
		"Movie 720p":       "720p",
		"Movie 480p":       "480p",
		"Show S01E01 HDTV": "HDTV",
		"Movie unknown":    "",
	}
	for title, want := range cases {
		if got := extractQuality(title); got != want {
			t.Errorf("%q: got %q want %q", title, got, want)
		}
	}
}

// TestSearchPerIndexerTimeout verifies the fan-out is concurrent and a
// hung indexer is bounded by perIndexerTO without blocking a healthy one.
func TestSearchPerIndexerTimeout(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, sampleFeed)
	}))
	defer fast.Close()

	// Slow server hangs until its request context is cancelled.
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer slow.Close()

	c := NewTorznabClient(StaticIndexers{
		{Name: "slow", URL: slow.URL + "/torznab"},
		{Name: "fast", URL: fast.URL + "/torznab"},
	}, nil)
	c.perIndexerTO = 150 * time.Millisecond

	start := time.Now()
	res, err := c.Search(context.Background(), Query{Type: MediaTypeMovie, IMDbID: "tt0133093"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("expected results from the fast indexer despite the slow one timing out")
	}
	if elapsed > time.Second {
		t.Errorf("slow indexer blocked the fan-out: took %v (per-indexer TO 150ms)", elapsed)
	}
}

func TestInfoHashFromMagnet(t *testing.T) {
	h := infoHashFromMagnet("magnet:?xt=urn:btih:ABCDEF&dn=x")
	if h != "abcdef" {
		t.Errorf("got %q", h)
	}
	if infoHashFromMagnet("magnet:?dn=x") != "" {
		t.Error("expected empty for magnet without btih")
	}
}
