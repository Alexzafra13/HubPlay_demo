package iptv

import (
	"testing"

	"hubplay/internal/db"
)

func TestApplyOrderOverlay_NoOverridesPassesThrough(t *testing.T) {
	channels := []*db.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
	}
	out := applyOrderOverlay(channels, nil)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("expected pass-through, got %+v", out)
	}
}

func TestApplyOrderOverlay_ReordersByUserPosition(t *testing.T) {
	channels := []*db.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
		{ID: "c", Name: "C", Number: 3},
	}
	// User puts channel C first, channel A last. B has no override.
	overrides := []db.UserChannelOrderEntry{
		{ChannelID: "c", Position: 1},
		{ChannelID: "a", Position: 99},
	}
	out := applyOrderOverlay(channels, overrides)
	if len(out) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(out))
	}
	// C(1) first, then B(2 — admin default), then A(99 — override).
	if out[0].ID != "c" {
		t.Errorf("position 0 = %q, want c", out[0].ID)
	}
	if out[1].ID != "b" {
		t.Errorf("position 1 = %q, want b", out[1].ID)
	}
	if out[2].ID != "a" {
		t.Errorf("position 2 = %q, want a", out[2].ID)
	}
}

func TestApplyOrderOverlay_HidesChannelsMarkedHidden(t *testing.T) {
	channels := []*db.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
		{ID: "c", Name: "C", Number: 3},
	}
	overrides := []db.UserChannelOrderEntry{
		{ChannelID: "b", Position: 2, Hidden: true},
	}
	out := applyOrderOverlay(channels, overrides)
	if len(out) != 2 {
		t.Fatalf("expected 2 visible channels, got %d", len(out))
	}
	for _, ch := range out {
		if ch.ID == "b" {
			t.Errorf("hidden channel b leaked through the overlay")
		}
	}
}

func TestApplyOrderOverlay_StableOrderAmongTies(t *testing.T) {
	// Two channels share the same admin Number — neither has an
	// override. They should keep their original relative order.
	channels := []*db.Channel{
		{ID: "first", Name: "F", Number: 1},
		{ID: "second", Name: "S", Number: 1},
	}
	out := applyOrderOverlay(channels, []db.UserChannelOrderEntry{
		// Unrelated override for a non-existent channel so the
		// function takes the overlay path (not the fast nil-path).
		{ChannelID: "ghost", Position: 99},
	})
	if out[0].ID != "first" || out[1].ID != "second" {
		t.Errorf("stable sort broken: got %q,%q", out[0].ID, out[1].ID)
	}
}

func TestApplyOrderOverlay_DoesNotMutateInput(t *testing.T) {
	channels := []*db.Channel{
		{ID: "a", Number: 10},
	}
	overrides := []db.UserChannelOrderEntry{
		{ChannelID: "a", Position: 1},
	}
	_ = applyOrderOverlay(channels, overrides)
	if channels[0].Number != 10 {
		t.Errorf("input channel was mutated: Number = %d, want 10", channels[0].Number)
	}
}
