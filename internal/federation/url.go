package federation

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"hubplay/internal/domain"
)

// maxFederationRedirects caps how many 3xx hops an outbound peer call
// will follow. The federation protocol itself never redirects (every
// peer endpoint serves its bytes directly), so a handful is plenty of
// slack for a reverse proxy doing http→https or trailing-slash
// normalisation without opening a redirect-chain amplification vector.
const maxFederationRedirects = 5

// federationCheckRedirect is the net/http CheckRedirect hook for the
// federation HTTP client. Without it, Go's default client follows up
// to 10 redirects to ANY address — a hostile-but-paired peer could
// answer a poster / stream / browse fetch with `302 → http://169.254.169.254/…`
// (cloud metadata, link-local) or an internal service and have us
// proxy the response back. validatePeerURL only gates the INITIAL URL;
// this re-runs the same SSRF gate (loopback / link-local / unspecified
// / multicast blocked, RFC1918 still allowed for LAN federation) on
// every redirect hop. F-1 (audit 2026-06-12).
//
// Not DNS-rebinding-proof on its own (the lookup here races the dial),
// but closes the metadata/internal-service reach that motivated the
// finding; the peer is already admin-paired, so this is defence in
// depth, not a primary trust boundary.
func federationCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxFederationRedirects {
		return fmt.Errorf("%w: too many redirects (%d)", domain.ErrPeerURLUnsafe, len(via))
	}
	return validatePeerURL(req.URL.String())
}

// validatePeerURL is the SSRF gate for peer-controlled URLs. Called
// when persisting `peer.BaseURL` from a remote handshake (HandleInboundHandshake)
// or from an admin-pasted URL (AcceptInvite). Defense in depth: even
// the admin-paste path is validated so a typo or a phishing link
// pasted in the admin UI doesn't end up making us probe localhost.
//
// Rejects:
//   - empty / unparseable URLs
//   - schemes other than http/https
//   - URLs whose host resolves to loopback (127.0.0.0/8, ::1)
//   - URLs whose host resolves to link-local (169.254/16, fe80::/10)
//   - URLs whose host resolves to unspecified (0.0.0.0, ::)
//   - URLs whose host resolves to multicast
//
// Does NOT reject RFC1918 private addresses. Two reasons:
//
//  1. The integration test rig (`docker-compose.federation-test.yml`)
//     uses Docker bridge networking, which puts containers on
//     172.17/16. Blocking RFC1918 would prevent the canonical
//     two-server local test setup.
//  2. Homelab federation between two HubPlay instances on the same LAN
//     (e.g. 192.168.x.x ↔ 192.168.x.y) is a legitimate deployment.
//
// The threat closed here is "hostile peer with a valid invite probes
// local services": loopback is the dangerous probe target. RFC1918 is
// a wider attack surface but lower-impact (the peer needs to guess a
// reachable internal IP and gets only a TCP-connect signal back, since
// the Ed25519 signature on responses can't be forged).
//
// Future hardening: a `federation.strict_url_validation` config flag
// could opt in to also blocking IsPrivate(). Out of v1 scope.
func validatePeerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("%w: empty URL", domain.ErrPeerURLUnsafe)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parse: %v", domain.ErrPeerURLUnsafe, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q (must be http or https)", domain.ErrPeerURLUnsafe, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", domain.ErrPeerURLUnsafe)
	}

	// If the host is already a literal IP, check it directly without
	// a DNS roundtrip. Otherwise resolve and check every returned
	// address (multi-A records, CNAME chains, IPv4+IPv6 dual stack).
	if ip := net.ParseIP(host); ip != nil {
		if blockedPeerIP(ip) {
			return fmt.Errorf("%w: %s", domain.ErrPeerURLUnsafe, ip)
		}
		return nil
	}

	addrs, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("%w: resolve %s: %v", domain.ErrPeerURLUnsafe, host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %s resolved to no addresses", domain.ErrPeerURLUnsafe, host)
	}
	for _, ip := range addrs {
		if blockedPeerIP(ip) {
			return fmt.Errorf("%w: %s resolves to %s", domain.ErrPeerURLUnsafe, host, ip)
		}
	}
	return nil
}

// blockedPeerIP reports whether ip is in a range we refuse to talk to
// from federation outbound calls. Narrower than imaging.BlockedIP
// (which also blocks RFC1918) because federation legitimately runs on
// LANs and inside docker bridges.
//
// Test seam: assigned via a var so tests that hit an httptest.Server
// on 127.0.0.1 can swap it. Production callers MUST NOT reassign.
var blockedPeerIP = defaultBlockedPeerIP

func defaultBlockedPeerIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast()
}
