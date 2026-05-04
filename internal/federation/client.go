package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Outbound peer-to-peer calls. Implemented as methods on *Manager
// (FetchPeerLibraries, FetchPeerItems) since they share the same
// identity, http.Client, and peer-cache state. Authentication: every
// request sets `Authorization: Bearer <jwt>` where the token is
// freshly minted via Manager.IssuePeerToken; the remote's
// RequirePeerJWT middleware validates it against the pubkey it
// pinned at handshake.
//
// Errors: non-2xx responses are decoded into the standard error
// envelope and surfaced as fmt.Errorf with the remote's code+message,
// so the caller's logs see WHY the peer rejected (rate-limited,
// scope insufficient, peer revoked, etc.).

// remoteSharedLibrary mirrors the JSON the peer emits at GET
// /peer/libraries. Kept here (rather than imported from the handlers
// package) to avoid a federation→handlers import cycle.
type remoteSharedLibrary struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ContentType string      `json:"content_type"`
	Scopes      ShareScopes `json:"scopes"`
}

type remoteSharedItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Overview  string `json:"overview,omitempty"`
	HasPoster bool   `json:"has_poster,omitempty"`
}

type remoteItemsResponse struct {
	Items []remoteSharedItem `json:"items"`
	Total int                `json:"total"`
}

// remoteErrorResponse mirrors the error envelope responseError emits
// in the handlers package. We decode this on non-2xx so the user-
// facing surface can show meaningful errors ("rate limited",
// "library not found", etc.).
type remoteErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// FetchPeerLibraries hits the remote's GET /peer/libraries endpoint.
// Returns the libraries the calling peer (us) has been granted via
// shares. Errors include rate-limit, peer-offline, etc.
func (m *Manager) FetchPeerLibraries(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}

	url, err := joinBaseURL(peer.BaseURL, "/api/v1/peer/libraries")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch libraries from peer %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeRemoteError(resp)
	}

	var wire []remoteSharedLibrary
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode libraries: %w", err)
	}
	out := make([]*SharedLibrary, 0, len(wire))
	for _, w := range wire {
		out = append(out, &SharedLibrary{
			ID:          w.ID,
			Name:        w.Name,
			ContentType: w.ContentType,
			Scopes:      w.Scopes,
		})
	}
	return out, nil
}

// SharedLibraryWithPeer pairs a SharedLibrary with the peer that
// shares it. Powers the unified "all libraries from all peers" view
// in the user-facing UI — fetched per-peer in parallel by
// FetchAllPeerLibraries.
type SharedLibraryWithPeer struct {
	Peer    *Peer
	Library *SharedLibrary
}

// FetchPeerItems hits the remote's paginated catalog browse.
func (m *Manager) FetchPeerItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, 0, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, 0, fmt.Errorf("peer %s not paired", peerID)
	}

	url, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/libraries/%s/items?offset=%d&limit=%d", libraryID, offset, limit))
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch items from peer %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, decodeRemoteError(resp)
	}

	var wire remoteItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, 0, fmt.Errorf("decode items: %w", err)
	}
	out := make([]*SharedItem, 0, len(wire.Items))
	for _, w := range wire.Items {
		out = append(out, &SharedItem{
			ID:        w.ID,
			Type:      w.Type,
			Title:     w.Title,
			Year:      w.Year,
			Overview:  w.Overview,
			HasPoster: w.HasPoster,
		})
	}
	return out, wire.Total, nil
}

func decodeRemoteError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var er remoteErrorResponse
	if err := json.Unmarshal(body, &er); err == nil && er.Error.Code != "" {
		return fmt.Errorf("peer rejected: %d %s -- %s", resp.StatusCode, er.Error.Code, er.Error.Message)
	}
	return fmt.Errorf("peer status %d: %s", resp.StatusCode, body)
}

// PeerStreamSessionRequest is the JSON body POSTed by us (peer A) to a
// remote peer (peer B) when a local user clicks play on a remote item.
// The capability shape mirrors what stream.CapabilitiesFromRequest
// produces from the X-Hubplay-Client-Capabilities header on a direct
// client request -- so when peer B builds its waterfall decision, it
// sees our user's caps verbatim and a Kotlin TV / Chromecast that
// supports HEVC+EAC3+MKV gets DirectPlay through federation just as it
// would talking to its own server.
type PeerStreamSessionRequest struct {
	Profile      string                  `json:"profile,omitempty"` // initial transcode profile name
	Capabilities *PeerStreamCapabilities `json:"client_capabilities,omitempty"`
}

// PeerStreamCapabilities is the wire shape of stream.Capabilities,
// duplicated here to avoid the federation->stream import cycle.
// Conversion happens at the handler boundary on peer B's side.
type PeerStreamCapabilities struct {
	Video     []string `json:"video,omitempty"`
	Audio     []string `json:"audio,omitempty"`
	Container []string `json:"container,omitempty"`
}

// PeerStreamSessionResponse is the body the remote peer returns when
// it has spawned (or attached to) a streaming session for the request.
// MasterPath is what we feed back to our local client, rewritten to
// proxy through us -- we never expose the remote's hostname or peer
// JWT to the user's browser.
type PeerStreamSessionResponse struct {
	SessionID  string `json:"session_id"`
	Method     string `json:"method"` // "direct_play" | "direct_stream" | "transcode"
	MasterPath string `json:"master_path"`
}

// StartPeerStreamSession asks a paired peer to spawn a streaming
// session for one of its items, on behalf of one of our users. The
// remote peer uses its own stream.Manager (so its transcode budget
// caps + hwaccel apply); we just get back a session id we'll proxy
// HLS requests against.
//
// Errors: peer offline, peer revoked, item not in a shared library,
// remote-side transcode budget full -- all surface via decodeRemoteError
// so the caller's log sees the actual remote refusal.
func (m *Manager) StartPeerStreamSession(ctx context.Context, peerID, itemID string, body PeerStreamSessionRequest) (*PeerStreamSessionResponse, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}

	url, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/stream/%s/session", itemID))
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream session request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
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
		return nil, fmt.Errorf("start peer stream session %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeRemoteError(resp)
	}
	var out PeerStreamSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return &out, nil
}

// ProxyPeerItemPoster fetches a peer item's poster bytes through
// authenticated GET /api/v1/peer/items/{itemId}/poster. The remote
// re-verifies the calling peer (us) still has CanBrowse on the item's
// library, so a peer that lost a share can no longer read posters
// even if it cached the item id locally.
//
// Caller MUST defer resp.Body.Close(); the response is not buffered.
func (m *Manager) ProxyPeerItemPoster(ctx context.Context, peerID, itemID string) (*http.Response, error) {
	return m.ProxyPeerStreamRequest(ctx, peerID, fmt.Sprintf("/api/v1/peer/items/%s/poster", itemID))
}

// ProxyPeerStreamRequest issues a GET against `path` on the remote
// peer with our peer JWT and returns the live response. Callers MUST
// `defer resp.Body.Close()` and stream bytes from `resp.Body` to the
// user; the response is not buffered.
//
// Used for HLS manifest + segment proxying after StartPeerStreamSession.
// The remote `path` is whatever MasterPath the session response
// returned (or a manifest's relative reference resolved against it).
func (m *Manager) ProxyPeerStreamRequest(ctx context.Context, peerID, path string) (*http.Response, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}

	url, err := joinBaseURL(peer.BaseURL, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	tok, err := m.IssuePeerToken(peerID)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy peer stream %s: %w", peerID, err)
	}
	return resp, nil
}

