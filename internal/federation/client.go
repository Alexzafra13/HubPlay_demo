package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client makes outbound peer-to-peer calls. The Manager owns one
// per process; callers reach it via Manager.RemoteCatalog (Phase 4)
// and Manager.RemoteStream (Phase 5).
//
// Authentication: every request sets `Authorization: Bearer <jwt>`
// where the token is freshly minted with Manager.IssuePeerToken.
// The remote's RequirePeerJWT middleware validates it against the
// pubkey it pinned at handshake.
//
// Errors: non-2xx responses are decoded into the standard error
// envelope and surfaced as fmt.Errorf with the remote's code+message,
// so the caller's logs see WHY the peer rejected (rate-limited,
// scope insufficient, peer revoked, etc.).
type peerClient struct {
	mgr *Manager
}

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
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Overview string `json:"overview,omitempty"`
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
			ID:       w.ID,
			Type:     w.Type,
			Title:    w.Title,
			Year:     w.Year,
			Overview: w.Overview,
		})
	}
	return out, wire.Total, nil
}

func decodeRemoteError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var er remoteErrorResponse
	if err := json.Unmarshal(body, &er); err == nil && er.Error.Code != "" {
		return fmt.Errorf("peer rejected: %d %s — %s", resp.StatusCode, er.Error.Code, er.Error.Message)
	}
	return fmt.Errorf("peer status %d: %s", resp.StatusCode, body)
}

// silence unused warning in trim builds where the type isn't referenced.
var _ peerClient
