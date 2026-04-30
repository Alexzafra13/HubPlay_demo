package federation

import (
	"context"
	"errors"
	"net"
	"testing"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

func TestValidatePeerURL_AcceptsPublicHTTPS(t *testing.T) {
	// External hostname that resolves to a public IP. Use a well-known
	// stable host. We don't actually contact it — net.LookupIP only.
	if err := validatePeerURL("https://example.com"); err != nil {
		t.Fatalf("public URL should be accepted, got: %v", err)
	}
}

func TestValidatePeerURL_AcceptsLiteralPublicIP(t *testing.T) {
	// Literal public IP, no DNS roundtrip. 1.1.1.1 is a routable public
	// address; the validator must accept it.
	if err := validatePeerURL("https://1.1.1.1:8443"); err != nil {
		t.Fatalf("literal public IP should be accepted, got: %v", err)
	}
}

func TestValidatePeerURL_AcceptsRFC1918(t *testing.T) {
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
	addr := "http://169.254.0.1:8096"
	if err := validatePeerURL(addr); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("%s should be rejected (link-local), got: %v", addr, err)
	}
}

func TestValidatePeerURL_RejectsUnspecified(t *testing.T) {
	addr := "http://0.0.0.0:8096"
	if err := validatePeerURL(addr); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("%s should be rejected (unspecified), got: %v", addr, err)
	}
}

func TestValidatePeerURL_RejectsEmpty(t *testing.T) {
	if err := validatePeerURL(""); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("empty URL should be rejected, got: %v", err)
	}
}

func TestValidatePeerURL_RejectsBadScheme(t *testing.T) {
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
	if err := validatePeerURL("http://"); !errors.Is(err, domain.ErrPeerURLUnsafe) {
		t.Errorf("URL without host should be rejected, got: %v", err)
	}
}

// TestValidatePeerURL_TestSeamRespected pins the contract that tests
// can swap the IP-block predicate. If this contract changes (e.g. the
// var becomes a constant), the integration test rig stops being able
// to wire httptest.Server URLs.
func TestValidatePeerURL_TestSeamRespected(t *testing.T) {
	saved := blockedPeerIP
	blockedPeerIP = func(_ net.IP) bool { return false }
	defer func() { blockedPeerIP = saved }()

	if err := validatePeerURL("http://127.0.0.1:8096"); err != nil {
		t.Fatalf("with permissive predicate, loopback should pass: %v", err)
	}
}

// TestHandleInboundHandshake_RejectsHostileLoopbackAdvertisedURL
// exercises the security guarantee end-to-end: a remote peer with a
// valid invite that claims AdvertisedURL pointing at our localhost
// MUST be rejected before persistence. Otherwise pairing with the
// invite + then making us probe localhost is the SSRF.
func TestHandleInboundHandshake_RejectsHostileLoopbackAdvertisedURL(t *testing.T) {
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
