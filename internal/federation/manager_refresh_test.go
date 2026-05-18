package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// TestRefreshPeerBranding_HappyPath: el manager re-probea al peer
// remoto via httptest y persiste el branding nuevo (nombre + color
// + URL de la foto). Cubre que la firma del flow está bien cableada
// — el path del request, el decode del response y el UPDATE del
// repo se ejercen punta-a-punta.
func TestRefreshPeerBranding_HappyPath(t *testing.T) {
	ctx := context.Background()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Local"); err != nil {
		t.Fatal(err)
	}

	// Server fake del peer remoto: emite /federation/info con el
	// branding "nuevo" para verificar que el local lo capta.
	remotePub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	remoteUUID := "remote-uuid-1234"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/federation/info" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_uuid":         remoteUUID,
			"name":                "Renamed Server",
			"version":             "0.1.0",
			"public_key":          EncodePublicKey(remotePub),
			"pubkey_fingerprint":  "ignored",
			"pubkey_words":        []string{},
			"supported_scopes":    []string{"browse"},
			"advertised_url":      "http://example",
			"avatar_color":        "#be185d",
			"avatar_image_url":    "http://example/api/v1/federation/identity/avatar?v=server-aaaa.jpg",
		})
	}))
	t.Cleanup(srv.Close)

	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	// Seedeamos un peer paired con branding antiguo + base_url
	// apuntando al server fake.
	old := &Peer{
		ID:             "peer-1",
		ServerUUID:     remoteUUID,
		Name:           "Old Name",
		BaseURL:        srv.URL,
		PublicKey:      remotePub,
		Status:         PeerPaired,
		CreatedAt:      clk.Now(),
		AvatarColor:    "#0f766e",
		AvatarImageURL: "",
	}
	if err := repo.InsertPeer(ctx, old); err != nil {
		t.Fatal(err)
	}

	got, err := mgr.RefreshPeerBranding(ctx, "peer-1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got.Name != "Renamed Server" {
		t.Errorf("name = %q, want %q", got.Name, "Renamed Server")
	}
	if got.AvatarColor != "#be185d" {
		t.Errorf("avatar_color = %q, want %q", got.AvatarColor, "#be185d")
	}
	if got.AvatarImageURL == "" {
		t.Errorf("avatar_image_url should not be empty after refresh")
	}

	// Verificamos también que se persistió, no solo se devolvió.
	persisted, err := repo.GetPeerByID(ctx, "peer-1")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "Renamed Server" {
		t.Errorf("persisted name = %q, want %q", persisted.Name, "Renamed Server")
	}
	if persisted.AvatarColor != "#be185d" {
		t.Errorf("persisted color = %q, want %q", persisted.AvatarColor, "#be185d")
	}
}

// TestRefreshPeerBranding_RejectsUuidMismatch: si el remoto responde
// con un server_uuid distinto al pinneado, el refresh aborta — alguien
// podría haber tomado control de la URL y queremos que el admin lo
// note en vez de sobreescribir silenciosamente.
func TestRefreshPeerBranding_RejectsUuidMismatch(t *testing.T) {
	ctx := context.Background()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Local"); err != nil {
		t.Fatal(err)
	}

	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_uuid":      "ATTACKER-UUID",
			"name":             "Evil",
			"version":          "0.1.0",
			"public_key":       EncodePublicKey(pub2),
			"pubkey_words":     []string{},
			"supported_scopes": []string{},
			"advertised_url":   "http://attacker",
		})
	}))
	t.Cleanup(srv.Close)

	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	if err := repo.InsertPeer(ctx, &Peer{
		ID:         "peer-1",
		ServerUUID: "ORIGINAL-UUID",
		Name:       "Original",
		BaseURL:    srv.URL,
		PublicKey:  pub1,
		Status:     PeerPaired,
		CreatedAt:  clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	_, err = mgr.RefreshPeerBranding(ctx, "peer-1")
	if err == nil {
		t.Fatal("expected error on server_uuid mismatch")
	}
}

// TestRefreshPeerBranding_RejectsNonPaired: refresh de un peer
// pending o revoked no tiene sentido — devolvemos error explícito
// para que el handler responda 4xx en vez de gastar el round-trip.
func TestRefreshPeerBranding_RejectsNonPaired(t *testing.T) {
	ctx := context.Background()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Local"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := repo.InsertPeer(ctx, &Peer{
		ID:         "peer-1",
		ServerUUID: "uuid",
		Name:       "name",
		PublicKey:  pub,
		Status:     PeerRevoked,
		CreatedAt:  clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.RefreshPeerBranding(ctx, "peer-1"); err == nil {
		t.Fatal("expected error refreshing revoked peer")
	}
}
