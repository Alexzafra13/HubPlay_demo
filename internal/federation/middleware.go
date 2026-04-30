package federation

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/domain"
)

// peerCtxKey is the context key under which the validated Peer is
// stashed by RequirePeerJWT for downstream handlers to read.
type peerCtxKey struct{}

// PeerFromContext returns the *Peer the middleware validated for
// this request. Nil if the request didn't go through the middleware
// (handler called outside the auth chain).
func PeerFromContext(ctx context.Context) *Peer {
	p, _ := ctx.Value(peerCtxKey{}).(*Peer)
	return p
}

// withPeer is the inverse of PeerFromContext — used by tests and the
// middleware itself.
func withPeer(ctx context.Context, p *Peer) context.Context {
	return context.WithValue(ctx, peerCtxKey{}, p)
}

// RequirePeerJWT is a chi-style middleware that authenticates inbound
// peer-to-peer requests. It:
//
//  1. Reads the Authorization: Bearer <jwt> header.
//  2. Validates the JWT (issuer pinned to a known peer, audience = us,
//     signature matches peer's pubkey, not expired, peer not revoked).
//  3. Applies the per-peer token-bucket rate limiter.
//  4. Wraps the response writer so we can capture status + bytes for
//     the audit log.
//  5. Stashes the *Peer in context for downstream handlers.
//  6. After the handler returns, records an audit entry.
//
// On any failure: rejects with the matching status code, records the
// rejection in the audit log too (so a peer hammering with bad tokens
// is visible), and short-circuits.
//
// Failures are mapped to HTTP statuses:
//
//	401 missing or malformed Authorization header
//	403 known peer but JWT invalid (signature mismatch, expired,
//	    wrong audience, peer revoked)
//	429 rate limit exceeded
func RequirePeerJWT(mgr *Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			tok, err := extractBearerToken(r)
			if err != nil {
				writePeerError(w, r, http.StatusUnauthorized, "PEER_AUTH_REQUIRED", err.Error())
				return
			}
			ourUUID := mgr.identity.Current().ServerUUID
			claims, peer, err := ValidatePeerToken(mgr.clock, mgr, ourUUID, tok)
			if err != nil {
				status, code := mapValidationError(err)
				// Don't have a peer object yet — the token didn't resolve
				// to one. Audit only what we know: the endpoint + status.
				mgr.logger.Warn("federation: peer JWT rejected",
					"err", err, "status", status, "endpoint", r.URL.Path)
				writePeerError(w, r, status, code, "peer authentication failed")
				return
			}

			// Replay check — the JWT individually verifies (signature,
			// audience, expiry) but a captured token can be replayed
			// within its 5-minute TTL window. CheckAndStoreNonce returns
			// false on a duplicate and the request is rejected.
			tokenExp := mgr.clock.Now().Add(peerTokenTTL)
			if claims.ExpiresAt != nil {
				tokenExp = claims.ExpiresAt.Time
			}
			if !mgr.CheckAndStoreNonce(claims.Nonce, tokenExp) {
				mgr.recordAudit(AuditEntry{
					PeerID:     peer.ID,
					Method:     r.Method,
					Endpoint:   normaliseEndpoint(r.URL.Path),
					StatusCode: http.StatusUnauthorized,
					ErrorKind:  "replay",
					DurationMs: time.Since(start).Milliseconds(),
				})
				mgr.logger.Warn("federation: peer JWT replay rejected",
					"peer_id", peer.ID, "endpoint", r.URL.Path)
				writePeerError(w, r, http.StatusUnauthorized, "PEER_REPLAY",
					"token already used")
				return
			}

			// Rate limit AFTER auth — a hostile peer with a valid JWT
			// (token bucket per peer_id, not per IP) is the threat we
			// shape against; an attacker without a valid token never
			// passes step 1.
			if mgr.ratelimit != nil && !mgr.ratelimit.Allow(peer.ID) {
				w.Header().Set("Retry-After", "60")
				mgr.recordAudit(AuditEntry{
					PeerID:     peer.ID,
					Method:     r.Method,
					Endpoint:   normaliseEndpoint(r.URL.Path),
					StatusCode: http.StatusTooManyRequests,
					ErrorKind:  "rate_limited",
					DurationMs: time.Since(start).Milliseconds(),
				})
				writePeerError(w, r, http.StatusTooManyRequests, "PEER_RATE_LIMITED",
					"too many requests")
				return
			}

			// Wrap the response writer so we can capture status + bytes
			// for the audit log AFTER the handler runs.
			rec := &peerResponseRecorder{ResponseWriter: w, status: http.StatusOK}
			ctx := withPeer(r.Context(), peer)

			next.ServeHTTP(rec, r.WithContext(ctx))

			mgr.recordAudit(AuditEntry{
				PeerID:     peer.ID,
				Method:     r.Method,
				Endpoint:   normaliseEndpoint(r.URL.Path),
				StatusCode: rec.status,
				BytesOut:   rec.bytes,
				DurationMs: time.Since(start).Milliseconds(),
			})
		})
	}
}

// peerResponseRecorder is a thin wrapper that captures status code
// and bytes written for audit logging without altering response
// semantics. It does NOT buffer the body — bytes are still streamed
// straight to the client (large file downloads / SSE keep working).
type peerResponseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *peerResponseRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *peerResponseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// Implicit 200 (Go's net/http does this if you Write before
		// WriteHeader). Capture before bytes leave.
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Flush proxies http.Flusher so SSE endpoints in the peer surface
// (Phase 5+) work correctly.
func (r *peerResponseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// extractBearerToken pulls the JWT out of the Authorization header.
// Returns ErrPeerUnauthorized if the header is missing/malformed —
// the middleware maps that to 401.
func extractBearerToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", errors.New("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", errors.New("malformed Authorization header — expected 'Bearer <token>'")
	}
	tok := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if tok == "" {
		return "", errors.New("empty bearer token")
	}
	return tok, nil
}

// mapValidationError maps a JWT validation error to an HTTP status
// and an error code. The codes line up with the rest of the API so
// the failing peer's logs can correlate.
func mapValidationError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrPeerKeyMismatch):
		return http.StatusForbidden, "PEER_KEY_MISMATCH"
	case errors.Is(err, domain.ErrTokenExpired):
		return http.StatusForbidden, "PEER_TOKEN_EXPIRED"
	case errors.Is(err, domain.ErrPeerRevoked):
		return http.StatusForbidden, "PEER_REVOKED"
	case errors.Is(err, domain.ErrPeerUnauthorized):
		return http.StatusForbidden, "PEER_UNAUTHORIZED"
	case errors.Is(err, domain.ErrPeerNotFound):
		// Unknown issuer — could be MITM, could be a stale handshake.
		// 403 (not 404) to avoid confirming peer identity to attackers.
		return http.StatusForbidden, "PEER_UNKNOWN"
	default:
		return http.StatusUnauthorized, "PEER_AUTH_FAILED"
	}
}

// normaliseEndpoint replaces UUID-shaped path segments with `:id`
// placeholders so the audit log groups by endpoint pattern rather
// than blowing up cardinality with one row per (endpoint, item_id).
//
// Cheap heuristic: a 36-char segment with dashes at positions 8/13/
// 18/23 looks like a UUID. Keeps non-UUID segments verbatim.
func normaliseEndpoint(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if looksLikeUUID(p) {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	return true
}

// writePeerError emits a small JSON body so the calling peer's HTTP
// client surfaces a meaningful failure. Avoids the {"data":...} wrap
// of admin endpoints because peer-to-peer expects bare objects.
func writePeerError(w http.ResponseWriter, _ *http.Request, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := `{"error":{"code":` + strconv.Quote(code) + `,"message":` + strconv.Quote(msg) + `}}`
	_, _ = w.Write([]byte(body))
}
