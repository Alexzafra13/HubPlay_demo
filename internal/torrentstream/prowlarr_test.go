package torrentstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProwlarrTorznabURL(t *testing.T) {
	got := ProwlarrTorznabURL("http://localhost:9696/")
	want := "http://localhost:9696/api/v1/indexers/all/results/torznab"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestNormalizeIMDbAttr(t *testing.T) {
	cases := map[string]string{
		"0133093":   "tt0133093",
		"tt0133093": "tt0133093",
		"":          "",
		"abc":       "",
	}
	for in, want := range cases {
		if got := normalizeIMDbAttr(in); got != want {
			t.Errorf("normalizeIMDbAttr(%q): got %q want %q", in, got, want)
		}
	}
	// Falls through to the second candidate.
	if got := normalizeIMDbAttr("", "tt42"); got != "tt42" {
		t.Errorf("fallback candidate: got %q", got)
	}
}

func TestBuildTorznabURLCustomCategories(t *testing.T) {
	idx := TorznabIndexer{
		URL:              "http://localhost:9696/torznab",
		MovieCategories:  []string{"2040", "2050"},
		SeriesCategories: []string{"5040"},
	}
	raw, err := buildTorznabURL(idx, MediaTypeMovie, "tt1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "cat=2040%2C2050") {
		t.Errorf("movie cat not applied: %s", raw)
	}
	raw2, _ := buildTorznabURL(idx, MediaTypeSeries, "tt1", "")
	if !strings.Contains(raw2, "cat=5040") {
		t.Errorf("series cat not applied: %s", raw2)
	}
}

// capsServer serves a Torznab caps response, optionally a Prowlarr native
// indexer list, and can be told to return an <error> or a status code.
func capsServer(t *testing.T, errBody string, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/torznab", func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		if errBody != "" {
			_, _ = io.WriteString(w, errBody)
			return
		}
		_, _ = io.WriteString(w, `<?xml version="1.0"?><caps><server title="Prowlarr"/></caps>`)
	})
	mux.HandleFunc("/api/v1/indexer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"name":"1337x","enable":true},{"name":"Disabled","enable":false}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPingTorznab(t *testing.T) {
	srv := capsServer(t, "", 0)
	client := srv.Client()

	if err := pingTorznab(context.Background(), client, srv.URL+"/torznab", "key"); err != nil {
		t.Fatalf("healthy caps should ping ok: %v", err)
	}

	// Torznab <error> (e.g. bad api key) even with HTTP 200.
	bad := capsServer(t, `<error code="100" description="Incorrect user credentials" />`, 0)
	if err := pingTorznab(context.Background(), bad.Client(), bad.URL+"/torznab", "key"); err == nil {
		t.Fatal("expected error for torznab <error> body")
	}

	// HTTP 500.
	down := capsServer(t, "", http.StatusInternalServerError)
	if err := pingTorznab(context.Background(), down.Client(), down.URL+"/torznab", "key"); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestTestTorznabConnection(t *testing.T) {
	srv := capsServer(t, "", 0)
	if err := TestTorznabConnection(context.Background(), srv.URL+"/torznab", "key"); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
}

func TestClientStatus(t *testing.T) {
	good := capsServer(t, "", 0)
	bad := capsServer(t, "", http.StatusInternalServerError)

	c := NewTorznabClient([]TorznabIndexer{
		{Name: "prowlarr", URL: good.URL + "/torznab", BaseURL: good.URL, APIKey: "k"},
		{Name: "broken", URL: bad.URL + "/torznab"},
	}, nil)

	st := c.Status(context.Background())
	if len(st) != 2 {
		t.Fatalf("status len: got %d want 2", len(st))
	}
	if !st[0].Reachable {
		t.Errorf("first indexer should be reachable: %+v", st[0])
	}
	// Reachable + BaseURL set → trackers enumerated from native API.
	if len(st[0].Trackers) != 1 || st[0].Trackers[0] != "1337x" {
		t.Errorf("expected enabled tracker list, got %v", st[0].Trackers)
	}
	if st[0].HasAPIKey != true {
		t.Error("HasAPIKey should be true")
	}
	if st[1].Reachable {
		t.Errorf("second indexer should be unreachable: %+v", st[1])
	}
	if st[1].Error == "" {
		t.Error("unreachable indexer should carry an error message")
	}
}

func TestInstancesNoSecrets(t *testing.T) {
	c := NewTorznabClient([]TorznabIndexer{
		{Name: "p", URL: "http://x/torznab", APIKey: "secret"},
	}, nil)
	infos := c.Instances()
	if len(infos) != 1 || !infos[0].HasAPIKey {
		t.Fatalf("instances: %+v", infos)
	}
	// The struct has no api key field at all — nothing to leak.
}
