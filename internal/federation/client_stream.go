package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PeerStreamCaps mirrors stream.Capabilities's wire shape (slices, not
// maps) so we can serialise them in the session-start body without a
// dependency cycle on the stream package.
type PeerStreamCaps struct {
	Video     []string `json:"video,omitempty"`
	Audio     []string `json:"audio,omitempty"`
	Container []string `json:"container,omitempty"`
}

// PeerStreamSessionResult is what RequestPeerStream returns to the
// caller (the viewer-side handler / hook). The `MasterPlaylistURL`
// is the ORIGIN's URL, which the caller must REWRITE to point at
// its own /federated-stream/ proxy before serving to the user's
// player.
type PeerStreamSessionResult struct {
	SessionID         string
	MasterPlaylistURL string
	Method            string
	Container         string
}

// RequestPeerStream hits the remote's POST /peer/stream/{itemID}/session.
// Wraps the JWT minting + body serialisation + error decode so the
// viewer-side handler stays declarative.
//
// `remoteUserID` is the local user id of the calling user — the
// origin uses it to:
//
//   - dedupe: re-requests for the same (peerUser, item, profile) get
//     the same session.
//   - cap: enforces the per-peer concurrency limit per remote user.
//   - audit: federation_audit_log tags the session-start with this id.
//
// `caps` may be nil — the origin will fall back to default web-browser
// codec assumptions, which is the same legacy behaviour the local
// stream waterfall used before capability negotiation landed.
func (m *Manager) RequestPeerStream(ctx context.Context, peerID, remoteUserID, itemID, profile string, caps *PeerStreamCaps) (*PeerStreamSessionResult, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}

	endpoint, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/stream/%s/session", itemID))
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"remote_user_id": remoteUserID,
	}
	if profile != "" {
		body["profile"] = profile
	}
	if caps != nil {
		body["client_capabilities"] = caps
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream-session body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request peer stream %s: %w", peerID, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, decodeRemoteError(resp)
	}

	var wire struct {
		SessionID         string `json:"session_id"`
		MasterPlaylistURL string `json:"master_playlist_url"`
		Method            string `json:"method"`
		Container         string `json:"container,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return &PeerStreamSessionResult{
		SessionID:         wire.SessionID,
		MasterPlaylistURL: wire.MasterPlaylistURL,
		Method:            wire.Method,
		Container:         wire.Container,
	}, nil
}

// StopPeerStream tells the origin to drop the session. Best-effort —
// failures are logged by the caller but don't propagate, since the
// origin's idle sweep would reap the session anyway.
func (m *Manager) StopPeerStream(ctx context.Context, peerID, sessionID string) error {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return err
	}
	if peer == nil || peer.Status != PeerPaired {
		return fmt.Errorf("peer %s not paired", peerID)
	}
	endpoint, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/stream/session/%s", sessionID))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := m.httpClt.Do(req)
	if err != nil {
		return fmt.Errorf("stop peer stream: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 500 {
		return decodeRemoteError(resp)
	}
	return nil
}

// ProxyPeerStreamRequest forwards a viewer-side HLS request (master /
// variant playlist / segment) to the origin peer's identical endpoint
// and copies the response back. Used by the /federated-stream/...
// handlers — they hand us the full sub-path and we sign + forward.
//
// For master playlists, the caller is responsible for REWRITING URLs
// inside the response body so they point at the local proxy. This
// function is byte-transparent — it doesn't parse or transform the
// payload.
func (m *Manager) ProxyPeerStreamRequest(ctx context.Context, peerID, subPath string, w http.ResponseWriter) error {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return err
	}
	if peer == nil || peer.Status != PeerPaired {
		return fmt.Errorf("peer %s not paired", peerID)
	}
	endpoint, err := joinBaseURL(peer.BaseURL, "/api/v1"+subPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := m.httpClt.Do(req)
	if err != nil {
		return fmt.Errorf("proxy %s: %w", subPath, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Copy the upstream's relevant headers + status before streaming
	// the body. Skip hop-by-hop headers (Connection, Keep-Alive,
	// etc.) per RFC 7230 §6.1.
	for _, h := range []string{"Content-Type", "Content-Length", "Cache-Control", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, copyErr := io.Copy(w, resp.Body)
	return copyErr
}

// FetchPeerMasterPlaylist hits the origin's master.m3u8 endpoint,
// reads the body, and returns it as bytes. Separated from the
// streaming proxy because the master playlist body needs URL
// REWRITING before it goes to the user's player — variant URLs in
// the response point at the origin and would bypass the proxy.
//
// The caller substitutes those URLs with local /federated-stream/
// equivalents and writes the rewritten body back to the user.
func (m *Manager) FetchPeerMasterPlaylist(ctx context.Context, peerID, sessionID string) (string, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return "", err
	}
	if peer == nil || peer.Status != PeerPaired {
		return "", fmt.Errorf("peer %s not paired", peerID)
	}
	endpoint, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/stream/session/%s/master.m3u8", sessionID))
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := m.httpClt.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch master playlist: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return "", decodeRemoteError(resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) // 64 KiB ceiling — playlists are tiny
	if err != nil {
		return "", fmt.Errorf("read master playlist: %w", err)
	}
	return string(body), nil
}

// RewritePeerMasterPlaylist takes a master playlist body (as fetched
// from a peer's /peer/stream/session/.../master.m3u8) and replaces
// the variant URLs with local proxy URLs. Each non-comment line is
// inspected — if it parses as an absolute URL, the host is replaced
// with localBase and the path is rewritten from
// `/api/v1/peer/stream/session/{sessionID}/{quality}/index.m3u8` to
// `/api/v1/me/peers/{peerID}/stream/session/{sessionID}/{quality}/index.m3u8`.
//
// Lines that don't parse cleanly are passed through unchanged — the
// player will fail predictably on a malformed URL, which is the
// correct behaviour over silently rewriting something we don't
// understand.
func RewritePeerMasterPlaylist(body, peerID, localBase string) string {
	localBase = strings.TrimRight(localBase, "/")
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		u, err := url.Parse(trimmed)
		if err != nil || u.Host == "" {
			// Relative URL (no host). Rewrite the path prefix only.
			rewritten := rewritePeerStreamPath(trimmed, peerID)
			b.WriteString(rewritten)
			b.WriteByte('\n')
			continue
		}
		// Absolute URL: rebuild with local host + rewritten path.
		newPath := rewritePeerStreamPath(u.Path, peerID)
		b.WriteString(localBase + newPath)
		b.WriteByte('\n')
	}
	// strings.Split + rejoin appends a trailing newline; trim only if
	// the original didn't end with one.
	out := b.String()
	if !strings.HasSuffix(body, "\n") && strings.HasSuffix(out, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// rewritePeerStreamPath transforms the origin's session-scoped path
// into the local proxy's equivalent. The variant URL pattern is fixed
// so a string replacement is sufficient.
func rewritePeerStreamPath(p, peerID string) string {
	const originPrefix = "/api/v1/peer/stream/"
	const localPrefixFmt = "/api/v1/me/peers/%s/stream/"
	if strings.HasPrefix(p, originPrefix) {
		return fmt.Sprintf(localPrefixFmt, peerID) + strings.TrimPrefix(p, originPrefix)
	}
	return p
}
