package torrentstream

import (
	"strings"
	"testing"
)

func TestParseArchiveResponse(t *testing.T) {
	// Two docs: one with a string year, one with a numeric year (IA
	// returns both shapes), plus one with an empty identifier that must
	// be dropped.
	body := `{
	  "response": {
	    "numFound": 3,
	    "docs": [
	      {"identifier": "sintel", "title": "Sintel", "mediatype": "movies", "year": "2010"},
	      {"identifier": "bbb", "title": "Big Buck Bunny", "mediatype": "movies", "year": 2008},
	      {"identifier": "", "title": "junk", "mediatype": "movies"}
	    ]
	  }
	}`

	results, err := parseArchiveResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results: got %d want 2 (empty identifier dropped)", len(results))
	}

	if results[0].Identifier != "sintel" || results[0].Year != "2010" {
		t.Errorf("doc0: %+v", results[0])
	}
	if results[0].TorrentURL != "https://archive.org/download/sintel/sintel_archive.torrent" {
		t.Errorf("doc0 torrent URL: %q", results[0].TorrentURL)
	}
	if results[1].Identifier != "bbb" || results[1].Year != "2008" {
		t.Errorf("doc1 (numeric year): %+v", results[1])
	}
}

func TestParseArchiveResponseEmpty(t *testing.T) {
	results, err := parseArchiveResponse(strings.NewReader(`{"response":{"numFound":0,"docs":[]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}
