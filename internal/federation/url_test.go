package federation

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// TestFederationCheckRedirect_BlocksLoopbackHop is the F-1 regression:
// a redirect to a blocked address (loopback / link-local) must abort
// the request. A hostile peer 302-ing a fetch toward cloud metadata
// (169.254.169.254 is link-local) or our own localhost is the SSRF.
func TestFederationCheckRedirect_BlocksLoopbackHop(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9999/evil", nil)
	if err := federationCheckRedirect(req, nil); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("loopback redirect target should be blocked, got: %v", err)
	}
	linklocal, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err := federationCheckRedirect(linklocal, nil); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("link-local (cloud metadata) redirect should be blocked, got: %v", err)
	}
}

// TestFederationCheckRedirect_CapsHops bounds redirect-chain length.
func TestFederationCheckRedirect_CapsHops(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/x", nil)
	via := make([]*http.Request, maxFederationRedirects)
	if err := federationCheckRedirect(req, via); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("redirect past the cap should be rejected, got: %v", err)
	}
}

// TestFederationClient_RefusesRedirectToLoopback exercises the guard
// through a real *http.Client.Do: a server that 302s to loopback must
// surface an error instead of transparently proxying the internal hop.
func TestFederationClient_RefusesRedirectToLoopback(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a loopback target the client must refuse to follow.
		http.Redirect(w, r, "http://127.0.0.1:1/internal", http.StatusFound)
	}))
	defer upstream.Close()

	client := &http.Client{CheckRedirect: federationCheckRedirect}
	resp, err := client.Get(upstream.URL) //nolint:bodyclose // err path
	if err == nil {
		resp.Body.Close()
		t.Fatal("client should have refused the loopback redirect")
	}
	if !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("wrong error: %v (want ErrPeerURLUnsafe)", err)
	}
}

func TestValidatePeerURL_AcceptsPublicHTTPS(t *testing.T) {
	t.Parallel()
	// External hostname that resolves to a public IP. Use a well-known
	// stable host. We don't actually contact it — net.LookupIP only.
	if err := validatePeerURL("https://example.com"); err != nil {
		t.Fatalf("public URL should be accepted, got: %v", err)
	}
}

func TestValidatePeerURL_AcceptsLiteralPublicIP(t *testing.T) {
	t.Parallel()
	// Literal public IP, no DNS roundtrip. 1.1.1.1 is a routable public
	// address; the validator must accept it.
	if err := validatePeerURL("https://1.1.1.1:8443"); err != nil {
		t.Fatalf("literal public IP should be accepted, got: %v", err)
	}
}

func TestValidatePeerURL_AcceptsRFC1918(t *testing.T) {
	t.Parallel()
	// Homelab + docker-compose federation rely on RFC1918 working.
	for _, addr := range []string{
		"http://10.0.0.5:8096",
		"http://172.17.0.2:8096",
		"http://192.168.1.10:8096",
	} {
		if err := validatePeerURL(addr); err != nil {
			t.Errorf("%s should be accepted (RFC1918 is allowed for v1), got: %v", addr, err)
		}
	}
}

func TestValidatePeerURL_RejectsLoopback(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"http://127.0.0.1:8096",
		"http://127.0.0.5:8096",
		"http://[::1]:8096",
	} {
		err := validatePeerURL(addr)
		if err == nil {
			t.Errorf("%s should be rejected (loopback)", addr)
			continue
		}
		if !errors.Is(err, domain.ErrPeerURLUnsafe) {
			t.Errorf("%s wrong error: %v (want ErrPeerURLUnsafe)", addr, err)
		}
	}
}

func TestValidatePeerURL_RejectsLinkLocal(t *testing.T) {
	t.Parallel()
	addr := "http://169.254.0.1:8096"
	if err := validatePeerURL(addr); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("%s should be rejected (link-local), got: %v", addr, err)
	}
}

func TestValidatePeerURL_RejectsUnspecified(t *testing.T) {
	t.Parallel()
	addr := "http://0.0.0.0:8096"
	if err := validatePeerURL(addr); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("%s should be rejected (unspecified), got: %v", addr, err)
	}
}

func TestValidatePeerURL_RejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := validatePeerURL(""); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("empty URL should be rejected, got: %v", err)
	}
}

func TestValidatePeerURL_RejectsBadScheme(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"file:///etc/passwd",
		"ftp://example.com",
		"javascript:alert(1)",
		"://no-scheme",
	} {
		if err := validatePeerURL(addr); !errors.Is(err, domain.ErrPeerURLUnsafe) {
			t.Errorf("%s should be rejected (bad scheme), got: %v", addr, err)
		}
	}
}

func TestValidatePeerURL_RejectsMissingHost(t *testing.T) {
	t.Parallel()
	if err := validatePeerURL("http://"); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("URL without host should be rejected, got: %v", err)
	}
}

// TestValidatePeerURL_TestSeamRespected pins the contract that tests
// can swap the IP-block predicate. If this contract changes (e.g. the
// var becomes a constant), the integration test rig stops being able
// to wire httptest.Server URLs.
//
// NO t.Parallel: muta `blockedPeerIP` (var package-level) y los demás
// tests del fichero la leen vía `validatePeerURL`. Si corriera en
// paralelo, el race detector dispara — los seriales corren antes que
// los paralelos pausados, así la restauración del defer ocurre antes
// de que cualquier paralelo arranque.
func TestValidatePeerURL_TestSeamRespected(t *testing.T) {
	saved := blockedPeerIP
	blockedPeerIP = func(_ net.IP) bool { return false }
	defer func() { blockedPeerIP = saved }()

	if err := validatePeerURL("http://127.0.0.1:8096"); err != nil {
		t.Fatalf("with permissive predicate, loopback should pass: %v", err)
	}
}

// TestRevokePeer_FailClosedOnCacheRefreshError is the F-4 regression:
// if the post-revoke cache refresh fails (DB read error), the manager
// must still evict the peer from the in-memory cache so its JWTs stop
// validating. Otherwise a revoked peer keeps authorising until a later
// successful refresh.
func TestRevokePeer_FailClosedOnCacheRefreshError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.New()

	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "ServerA"); err != nil {
		t.Fatal(err)
	}
	// Seed a paired peer directly so NewManager's refresh caches it.
	repo.peers = append(repo.peers, &Peer{
		ID:         "peer-1",
		ServerUUID: "peer-1-uuid",
		Name:       "Peer One",
		BaseURL:    "https://peer.example.com",
		PublicKey:  ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)),
		Status:     PeerPaired,
	})

	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	// Sanity: the peer is in the auth cache before revoke.
	if _, err := mgr.LookupByServerUUID("peer-1-uuid"); err != nil {
		t.Fatalf("peer should be cached pre-revoke: %v", err)
	}

	// Force the refresh inside RevokePeer to fail. The fail-closed path
	// must evict the entry directly instead of leaving it Paired.
	repo.failListPeers = true
	if err := mgr.RevokePeer(ctx, "peer-1"); err != nil {
		t.Fatalf("RevokePeer returned error: %v", err)
	}

	if _, err := mgr.LookupByServerUUID("peer-1-uuid"); !errors.Is(err, domain.ErrPeerNotFound) {
		t.Errorf("revoked peer must be evicted from cache even when refresh fails; got err=%v", err)
	}
}

// TestHandleInboundHandshake_RejectsHostileLoopbackAdvertisedURL
// exercises the security guarantee end-to-end: a remote peer with a
// valid invite that claims AdvertisedURL pointing at our localhost
// MUST be rejected before persistence. Otherwise pairing with the
// invite + then making us probe localhost is the SSRF.
func TestHandleInboundHandshake_RejectsHostileLoopbackAdvertisedURL(t *testing.T) {
	t.Parallel()
	// Default predicate IS production; we want it active for this
	// test. No allowLoopbackForTests call here.
	t.Helper()

	repo := &inMemoryFedRepo{}
	ctx := context.Background()
	clk := clock.New()
	if _, err := LoadOrCreate(ctx, repo, clk, "ServerA"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(ctx, DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	inv, err := mgr.GenerateInvite(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Hostile peer claims AdvertisedURL pointing at our local network
	// admin panel. Pretty much any sane user — even one that fell for
	// a phishing invite leak — would never see this URL persisted.
	hostileRemote := &ServerInfo{
		ServerUUID:    "hostile-uuid",
		Name:          "Hostile",
		PublicKey:     []byte("33-bytes-pretending-to-be-a-pubkey-blob"),
		AdvertisedURL: "http://127.0.0.1:9999",
	}
	_, _, err = mgr.HandleInboundHandshake(ctx, inv.Code, hostileRemote)
	if err == nil {
		t.Fatal("hostile loopback URL should be rejected")
	}
	if !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("wrong error kind: %v (want ErrPeerURLUnsafe)", err)
	}

	// And the peer must NOT have been persisted — pairing failed pre-write.
	peers, _ := mgr.ListPeers(ctx)
	if len(peers) != 0 {
		t.Errorf("hostile peer should not have been persisted, got %d peers", len(peers))
	}
}
