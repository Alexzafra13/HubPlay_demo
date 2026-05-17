package iptv

// Admin overlay (applyAdminOverlay) tests + composition tests for
// admin → user layering. The user overlay tests already cover that
// surface; here we lock in the admin variant + the cross-overlay
// invariants (admin hide is a hard constraint, etc).

import (
	"testing"

	iptvmodel "hubplay/internal/iptv/model"
)

func TestApplyAdminOverlay_NoOverridesPassesThrough(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
	}
	out := applyAdminOverlay(channels, nil)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("expected pass-through, got %+v", out)
	}
}

func TestApplyAdminOverlay_ReordersByAdminPosition(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
		{ID: "c", Name: "C", Number: 3},
	}
	overrides := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "c", Position: 1},
		{ChannelID: "a", Position: 99},
	}
	out := applyAdminOverlay(channels, overrides)
	if len(out) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(out))
	}
	if out[0].ID != "c" || out[1].ID != "b" || out[2].ID != "a" {
		t.Errorf("order = %q,%q,%q; want c,b,a", out[0].ID, out[1].ID, out[2].ID)
	}
}

func TestApplyAdminOverlay_HidesChannelsMarkedHidden(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
		{ID: "c", Name: "C", Number: 3},
	}
	overrides := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "b", Position: 2, Hidden: true},
	}
	out := applyAdminOverlay(channels, overrides)
	if len(out) != 2 {
		t.Fatalf("expected 2 visible channels, got %d", len(out))
	}
	for _, ch := range out {
		if ch.ID == "b" {
			t.Errorf("admin-hidden channel b leaked through the overlay")
		}
	}
}

// Hidden hard-constraint: when the admin hides a channel, the user
// overlay cannot surface it back. This is the senior-call invariant
// that justifies running admin BEFORE user — admin removes the
// channel from the pipeline entirely; downstream the user can't
// even reference it.
func TestComposition_AdminHidden_UserCannotUnhide(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Name: "A", Number: 1},
		{ID: "b", Name: "B", Number: 2},
	}
	adminRows := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "b", Position: 2, Hidden: true},
	}
	userOverrides := []iptvmodel.UserChannelOrderEntry{
		// User explicitly tries to position channel b visible.
		{ChannelID: "b", Position: 1, Hidden: false},
	}

	// Compose: admin first, then user.
	afterAdmin := applyAdminOverlay(channels, adminRows)
	final := applyOrderOverlay(afterAdmin, userOverrides)

	for _, ch := range final {
		if ch.ID == "b" {
			t.Errorf("user overlay surfaced admin-hidden channel b — hard constraint violated")
		}
	}
	if len(final) != 1 || final[0].ID != "a" {
		t.Errorf("final list = %+v; want just channel a", final)
	}
}

// User can still hide MORE channels than the admin did. Layered
// filtering: admin hidden ∪ user hidden = effective hidden set.
func TestComposition_UserCanHideMore(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Number: 1},
		{ID: "b", Number: 2},
		{ID: "c", Number: 3},
	}
	adminRows := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "a", Position: 1, Hidden: true}, // admin hides a
	}
	userOverrides := []iptvmodel.UserChannelOrderEntry{
		{ChannelID: "c", Position: 3, Hidden: true}, // user also hides c
	}

	afterAdmin := applyAdminOverlay(channels, adminRows)
	final := applyOrderOverlay(afterAdmin, userOverrides)

	if len(final) != 1 || final[0].ID != "b" {
		t.Errorf("expected only b visible, got %+v", final)
	}
}

// User overrides position on top of admin order. Admin reorders
// 'b' to position 1; user accepts that AND moves a different channel.
func TestComposition_UserPositionOnTopOfAdminPosition(t *testing.T) {
	channels := []*iptvmodel.Channel{
		{ID: "a", Number: 1},
		{ID: "b", Number: 2},
		{ID: "c", Number: 3},
	}
	adminRows := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "b", Position: 1}, // admin promotes b to position 1
	}
	userOverrides := []iptvmodel.UserChannelOrderEntry{
		{ChannelID: "c", Position: 0}, // user pulls c to the top of their own view
	}

	afterAdmin := applyAdminOverlay(channels, adminRows)
	final := applyOrderOverlay(afterAdmin, userOverrides)

	if len(final) != 3 {
		t.Fatalf("expected 3 visible, got %d", len(final))
	}
	// User's c is at position 0, then admin's b at 1, then a at 1 (original Number).
	// Admin's b and a both at position 1 — stable sort keeps b first since
	// it appears earlier in the admin-overlay output.
	if final[0].ID != "c" {
		t.Errorf("position 0 = %q; want c", final[0].ID)
	}
}
