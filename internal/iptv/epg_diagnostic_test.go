package iptv

import (
	"testing"

	"hubplay/internal/db"
)

func TestSampleTvgIDs(t *testing.T) {
	t.Parallel()

	channels := []*db.Channel{
		{TvgID: "a.es"},
		{TvgID: ""},
		nil,
		{TvgID: "b.es"},
		{TvgID: "c.es"},
		{TvgID: ""},
		{TvgID: "d.es"},
	}

	t.Run("respects max", func(t *testing.T) {
		t.Parallel()
		got := sampleTvgIDs(channels, 2)
		want := []string{"a.es", "b.es"}
		if !equalStringSlices(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})

	t.Run("skips blanks and nils", func(t *testing.T) {
		t.Parallel()
		got := sampleTvgIDs(channels, 100)
		want := []string{"a.es", "b.es", "c.es", "d.es"}
		if !equalStringSlices(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})

	t.Run("max zero returns nil", func(t *testing.T) {
		t.Parallel()
		if got := sampleTvgIDs(channels, 0); got != nil {
			t.Fatalf("got %v want nil", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		if got := sampleTvgIDs(nil, 5); len(got) != 0 {
			t.Fatalf("got %v want empty", got)
		}
	})
}

func TestCountBlankTvgIDs(t *testing.T) {
	t.Parallel()

	channels := []*db.Channel{
		{TvgID: "a"}, {TvgID: ""}, nil, {TvgID: "b"}, {TvgID: ""},
	}
	if got, want := countBlankTvgIDs(channels), 3; got != want {
		t.Fatalf("got %d want %d", got, want)
	}
	if got, want := countBlankTvgIDs(nil), 0; got != want {
		t.Fatalf("nil input: got %d want %d", got, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
