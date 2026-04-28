package handlers

import (
	"testing"

	"hubplay/internal/db"
)

func TestLogoProxyURL(t *testing.T) {
	cases := []struct {
		name string
		ch   *db.Channel
		want string
	}{
		{
			name: "nil channel returns empty",
			ch:   nil,
			want: "",
		},
		{
			name: "no upstream logo returns empty",
			ch:   &db.Channel{ID: "ch-1", LogoURL: ""},
			want: "",
		},
		{
			// The DTO is what the frontend reads, so the contract
			// here is: when there IS an upstream URL we always rewrite
			// to the same-origin proxy path. The path itself ("/api/v1/
			// channels/{id}/logo") is what registers the route in
			// router.go, and the test pins both ends together.
			name: "upstream present rewrites to proxy URL",
			ch:   &db.Channel{ID: "ch-1", LogoURL: "https://lo1.in/sp/antena3hd.png"},
			want: "/api/v1/channels/ch-1/logo",
		},
		{
			name: "upstream URL does not leak into proxy URL",
			ch:   &db.Channel{ID: "ch-2", LogoURL: "https://i.imgur.com/secret-token.png"},
			want: "/api/v1/channels/ch-2/logo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := logoProxyURL(tc.ch)
			if got != tc.want {
				t.Errorf("logoProxyURL: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestToChannelDTO_LogoURLIsProxiedNotUpstream(t *testing.T) {
	// Regression guard: if anyone reverts toChannelDTO to set
	// LogoURL: ch.LogoURL the upstream URL would leak back into
	// the API and CSP would block image loads again.
	ch := &db.Channel{
		ID:        "ch-42",
		Name:      "Antena 3 HD",
		LibraryID: "lib-x",
		LogoURL:   "https://lo1.in/sp/antena3hd.png",
		IsActive:  true,
	}
	dto := toChannelDTO(ch, "/api/v1/channels/ch-42/stream")
	if dto.LogoURL == ch.LogoURL {
		t.Fatalf("DTO leaked upstream logo URL: %q", dto.LogoURL)
	}
	if dto.LogoURL != "/api/v1/channels/ch-42/logo" {
		t.Errorf("DTO logo_url: got %q want /api/v1/channels/ch-42/logo", dto.LogoURL)
	}
}
