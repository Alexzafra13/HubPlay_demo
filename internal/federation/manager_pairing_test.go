package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/event"
)

// stubRemote levanta un httptest.Server que se hace pasar por un
// peer remoto: emite /federation/info con el ServerInfo dado y
// acepta /federation/pairing-requests con 202. Para tests del
// happy path de SendPairingRequest sin tener que montar un segundo
// Manager.
type stubRemote struct {
	srv         *httptest.Server
	info        *ServerInfo
	receivedReq *pairingRequestBody
}

func newStubRemote(t *testing.T) *stubRemote {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &stubRemote{
		info: &ServerInfo{
			ServerUUID:     "remote-uuid",
			Name:           "Remote",
			Version:        "0.1.0",
			PublicKey:      pub,
			AdvertisedURL:  "http://example",
			AvatarColor:    "#1d4ed8",
			AvatarImageURL: "",
		},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/federation/info":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"server_uuid":      s.info.ServerUUID,
				"name":             s.info.Name,
				"version":          s.info.Version,
				"public_key":       EncodePublicKey(s.info.PublicKey),
				"pubkey_words":     []string{},
				"supported_scopes": []string{},
				"advertised_url":   s.info.AdvertisedURL,
				"avatar_color":     s.info.AvatarColor,
				"avatar_image_url": s.info.AvatarImageURL,
			})
		case "/api/v1/federation/pairing-requests":
			var body pairingRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			s.receivedReq = &body
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	// Ajusta la URL del ServerInfo del stub a la del httptest, para
	// que SendPairingRequest la persista bien.
	s.info.AdvertisedURL = s.srv.URL
	return s
}

func TestSendPairingRequest_HappyPath(t *testing.T) {
	allowLoopbackForTests(t)
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

	remote := newStubRemote(t)
	t.Cleanup(remote.srv.Close)

	pending, err := mgr.SendPairingRequest(ctx, remote.srv.URL, "admin-uid")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if pending.Direction != PendingDirectionOutgoing {
		t.Errorf("direction = %s, want outgoing", pending.Direction)
	}
	if pending.PeerServerUUID != "remote-uuid" {
		t.Errorf("server_uuid = %s, want remote-uuid", pending.PeerServerUUID)
	}
	if pending.PeerAvatarColor != "#1d4ed8" {
		t.Errorf("captured branding lost: %q", pending.PeerAvatarColor)
	}
	if remote.receivedReq == nil {
		t.Fatal("remote did not receive POST")
	}
	if remote.receivedReq.RequestID != pending.ID {
		t.Errorf("request_id mismatch: wire=%s persisted=%s", remote.receivedReq.RequestID, pending.ID)
	}
}

// TestSendPairingRequest_IdempotentOnDuplicate: pulsar enviar dos
// veces al mismo URL devuelve la misma peticion pendiente, no crea
// una segunda.
func TestSendPairingRequest_IdempotentOnDuplicate(t *testing.T) {
	allowLoopbackForTests(t)
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

	remote := newStubRemote(t)
	t.Cleanup(remote.srv.Close)

	first, err := mgr.SendPairingRequest(ctx, remote.srv.URL, "admin-uid")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.SendPairingRequest(ctx, remote.srv.URL, "admin-uid")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Errorf("second send produced new request (%s vs %s); should be idempotent", first.ID, second.ID)
	}
}

// TestHandleIncomingPairingRequest_HappyPath + publishes event.
func TestHandleIncomingPairingRequest_HappyPath(t *testing.T) {
	allowLoopbackForTests(t)
	ctx := context.Background()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Local"); err != nil {
		t.Fatal(err)
	}
	bus := event.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	received := make(chan event.Event, 1)
	unsub := bus.Subscribe(EventPairingRequestReceived, func(e event.Event) {
		received <- e
	})
	t.Cleanup(unsub)
	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, bus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	requester := &ServerInfo{
		ServerUUID:    "remote-uuid",
		Name:          "Remote",
		PublicKey:     pub,
		AdvertisedURL: "http://127.0.0.1:9999",
		AvatarColor:   "#15803d",
	}
	got, err := mgr.HandleIncomingPairingRequest(ctx, "req-1", "token-abc", requester)
	if err != nil {
		t.Fatal(err)
	}
	if got.Direction != PendingDirectionIncoming {
		t.Errorf("direction = %s", got.Direction)
	}
	if got.PeerAvatarColor != "#15803d" {
		t.Errorf("branding captured wrongly: %q", got.PeerAvatarColor)
	}

	select {
	case e := <-received:
		if e.Data["server_uuid"] != "remote-uuid" {
			t.Errorf("event payload missing server_uuid")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EventPairingRequestReceived not published within 2s")
	}
}

// TestAcceptPairingRequest_CreatesPeer: tras aceptar, hay Peer
// paired en el repo + se publica EventPeerLinked.
func TestAcceptPairingRequest_CreatesPeer(t *testing.T) {
	allowLoopbackForTests(t)
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
	requester := &ServerInfo{
		ServerUUID:    "remote-uuid",
		Name:          "Remote",
		PublicKey:     pub,
		AdvertisedURL: "http://127.0.0.1:9999",
	}
	if _, err := mgr.HandleIncomingPairingRequest(ctx, "req-1", "token", requester); err != nil {
		t.Fatal(err)
	}
	peer, err := mgr.AcceptPairingRequest(ctx, "req-1", "admin-uid")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if peer == nil || peer.Status != PeerPaired {
		t.Fatalf("expected paired peer, got %#v", peer)
	}
	// La peticion ahora es accepted (terminal); accept again debe
	// fallar.
	if _, err := mgr.AcceptPairingRequest(ctx, "req-1", "admin-uid"); err == nil {
		t.Error("expected error accepting already-accepted request")
	}
}

// TestSendPairingRequest_RejectsAlreadyPaired: si ya hay Peer
// paired con ese server_uuid, no enviamos otra peticion.
func TestSendPairingRequest_RejectsAlreadyPaired(t *testing.T) {
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

	remote := newStubRemote(t)
	t.Cleanup(remote.srv.Close)

	pub := remote.info.PublicKey
	if err := repo.InsertPeer(ctx, &Peer{
		ID:         "p1",
		ServerUUID: "remote-uuid",
		Name:       "Remote",
		BaseURL:    remote.srv.URL,
		PublicKey:  pub,
		Status:     PeerPaired,
		CreatedAt:  clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = mgr.SendPairingRequest(ctx, remote.srv.URL, "admin-uid")
	if err == nil {
		t.Fatal("expected ErrAlreadyExists when sending to already-paired peer")
	}
}
