package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"
)

// peerFetchAttempts: reintentos para GETs idempotentes contra un peer.
// Techo bajo para no bloquear al usuario mas de unos segundos.
const peerFetchAttempts = 3

// peerFetchBackoff: delay inicial entre reintentos. Se duplica cada intento
// (250ms -> 500ms -> 1s); worst-case ~1.75s, dentro del HTTPTimeout de 15s.
const peerFetchBackoff = 250 * time.Millisecond

// Llamadas outbound peer-to-peer. Cada request lleva Authorization: Bearer <jwt>
// firmado con nuestra privkey; el remoto valida contra el pubkey pineado.
// Errores non-2xx se decodifican al envelope estandar para loguear la razon.

// remoteSharedLibrary espeja el JSON de GET /peer/libraries.
// Definido aqui para evitar ciclo federation->handlers.
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
	LibraryID string `json:"library_id,omitempty"`
	// Swatches pre-extraidos del peer. Vacios si el peer es anterior
	// al plumbing de colores o si la imagen no tiene paleta.
	PosterColor      string `json:"poster_color,omitempty"`
	PosterColorMuted string `json:"poster_color_muted,omitempty"`
}

type remoteItemsResponse struct {
	Items []remoteSharedItem `json:"items"`
	Total int                `json:"total"`
}

// remoteErrorResponse espeja el envelope de error de los handlers.
// Se decodifica en non-2xx para mostrar errores significativos.
type remoteErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// FetchPeerLibraries obtiene las bibliotecas que el peer nos comparte.
// Reintenta fallos transitorios (ver doIdempotentPeerGET).
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
	resp, err := m.doIdempotentPeerGET(ctx, peerID, url, "libraries")
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

// SharedLibraryWithPeer empareja una biblioteca con el peer que la comparte.
// Alimenta la vista unificada "todas las bibliotecas de todos los peers".
type SharedLibraryWithPeer struct {
	Peer    *Peer
	Library *SharedLibrary
}

// FetchPeerItems obtiene items paginados del catalogo remoto.
// Misma politica de reintentos que FetchPeerLibraries.
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
	resp, err := m.doIdempotentPeerGET(ctx, peerID, url, "items")
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
			ID:               w.ID,
			Type:             w.Type,
			Title:            w.Title,
			Year:             w.Year,
			Overview:         w.Overview,
			HasPoster:        w.HasPoster,
			LibraryID:        w.LibraryID,
			PosterColor:      w.PosterColor,
			PosterColorMuted: w.PosterColorMuted,
		})
	}
	return out, wire.Total, nil
}

// FetchPeerRecent obtiene los items mas recientes del peer remoto.
func (m *Manager) FetchPeerRecent(ctx context.Context, peerID string, limit int) ([]*SharedItem, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}
	if limit <= 0 {
		limit = 12
	}

	url, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/recent?limit=%d", limit))
	if err != nil {
		return nil, err
	}
	resp, err := m.doIdempotentPeerGET(ctx, peerID, url, "recent")
	if err != nil {
		return nil, fmt.Errorf("recent peer %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeRemoteError(resp)
	}
	var wire remoteItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode recent results: %w", err)
	}
	out := make([]*SharedItem, 0, len(wire.Items))
	for _, w := range wire.Items {
		out = append(out, &SharedItem{
			ID:               w.ID,
			Type:             w.Type,
			Title:            w.Title,
			Year:             w.Year,
			Overview:         w.Overview,
			HasPoster:        w.HasPoster,
			LibraryID:        w.LibraryID,
			PosterColor:      w.PosterColor,
			PosterColorMuted: w.PosterColorMuted,
		})
	}
	return out, nil
}

// FetchPeerSearch busca items en el peer remoto. Misma politica de reintentos.
// El remoto aplica su propio ACL de shares.
func (m *Manager) FetchPeerSearch(ctx context.Context, peerID, query string, limit int) ([]*SharedItem, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.Status != PeerPaired {
		return nil, fmt.Errorf("peer %s not paired", peerID)
	}
	if limit <= 0 {
		limit = 25
	}

	url, err := joinBaseURL(peer.BaseURL, fmt.Sprintf("/api/v1/peer/search?q=%s&limit=%d",
		neturl.QueryEscape(query), limit))
	if err != nil {
		return nil, err
	}
	resp, err := m.doIdempotentPeerGET(ctx, peerID, url, "search")
	if err != nil {
		return nil, fmt.Errorf("search peer %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeRemoteError(resp)
	}
	var wire remoteItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	out := make([]*SharedItem, 0, len(wire.Items))
	for _, w := range wire.Items {
		out = append(out, &SharedItem{
			ID:               w.ID,
			Type:             w.Type,
			Title:            w.Title,
			Year:             w.Year,
			Overview:         w.Overview,
			HasPoster:        w.HasPoster,
			LibraryID:        w.LibraryID,
			PosterColor:      w.PosterColor,
			PosterColorMuted: w.PosterColorMuted,
		})
	}
	return out, nil
}

func decodeRemoteError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var er remoteErrorResponse
	if err := json.Unmarshal(body, &er); err == nil && er.Error.Code != "" {
		return fmt.Errorf("peer rejected: %d %s -- %s", resp.StatusCode, er.Error.Code, er.Error.Message)
	}
	// Fallback: el body no es el envelope documentado. Sanitizar para
	// no filtrar internals del proxy upstream en logs.
	return fmt.Errorf("peer status %d: %s", resp.StatusCode, sanitiseRemoteBody(body))
}

// sanitiseRemoteBody extrae un excerpt acotado y seguro para log del body
// opaco de un peer. Reemplaza control chars, trunca, y no propaga el original.
func sanitiseRemoteBody(body []byte) string {
	const maxExcerpt = 256
	total := len(body)
	if total == 0 {
		return "<empty>"
	}
	excerpt := body
	if total > maxExcerpt {
		excerpt = body[:maxExcerpt]
	}
	clean := make([]byte, 0, len(excerpt))
	for _, b := range excerpt {
		switch {
		case b == '\t' || b == ' ':
			clean = append(clean, ' ')
		case b < 0x20 || b == 0x7f:
			clean = append(clean, '.')
		default:
			clean = append(clean, b)
		}
	}
	if total > maxExcerpt {
		return fmt.Sprintf("%s… <%d bytes total>", clean, total)
	}
	return string(clean)
}

// doIdempotentPeerGET hace GET autenticado con reintentos. Solo para
// operaciones idempotentes. Politica: transport error/5xx reintenta,
// 4xx/2xx/3xx retorna inmediato. Backoff exponencial. Mint JWT fresco
// por intento (Ed25519 local, barato).
func (m *Manager) doIdempotentPeerGET(ctx context.Context, peerID, url, kind string) (*http.Response, error) {
	var lastErr error
	backoff := peerFetchBackoff
	for attempt := 0; attempt < peerFetchAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		tok, err := m.IssuePeerToken(ctx, peerID)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")

		resp, doErr := m.httpClt.Do(req)
		if doErr != nil {
			// No reintentar si el contexto ya se cancelo.
			if errors.Is(doErr, context.Canceled) || errors.Is(doErr, context.DeadlineExceeded) {
				return nil, doErr
			}
			m.metrics.OutboundRequest(kind, "transport_error")
			lastErr = doErr
			continue
		}
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			// Drenar para devolver la conexion al pool idle (net/http).
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
			m.metrics.OutboundRequest(kind, "5xx")
			lastErr = fmt.Errorf("peer status %d", resp.StatusCode)
			continue
		}
		switch {
		case resp.StatusCode >= 400:
			m.metrics.OutboundRequest(kind, "4xx")
		default:
			m.metrics.OutboundRequest(kind, "ok")
		}
		return resp, nil
	}
	if lastErr == nil {
		// Defensivo: peerFetchAttempts > 0 por construccion.
		lastErr = errors.New("peer request loop exited without attempt")
	}
	return nil, lastErr
}

// PeerStreamSessionRequest es el body JSON que enviamos al peer remoto
// cuando un usuario local da play. Las capabilities se pasan verbatim
// para que el peer tome la misma decision de waterfall que su propio server.
type PeerStreamSessionRequest struct {
	Profile      string                  `json:"profile,omitempty"` // initial transcode profile name
	Capabilities *PeerStreamCapabilities `json:"client_capabilities,omitempty"`
}

// PeerStreamCapabilities duplica stream.Capabilities para evitar
// ciclo federation->stream. La conversion ocurre en el handler de B.
type PeerStreamCapabilities struct {
	Video     []string `json:"video,omitempty"`
	Audio     []string `json:"audio,omitempty"`
	Container []string `json:"container,omitempty"`
}

// PeerStreamSessionResponse es la respuesta del peer al crear una sesion
// de streaming. MasterPath se reescribe para proxear a traves nuestro;
// nunca exponemos hostname ni JWT del peer al navegador.
type PeerStreamSessionResponse struct {
	SessionID  string `json:"session_id"`
	Method     string `json:"method"` // "direct_play" | "direct_stream" | "transcode"
	MasterPath string `json:"master_path"`
}

// StartPeerStreamSession pide al peer que cree una sesion de streaming
// para un item suyo. El peer usa su propio stream.Manager (budget/hwaccel).
// Errores se surfacean via decodeRemoteError.
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
	tok, err := m.IssuePeerToken(ctx, peerID)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClt.Do(req)
	if err != nil {
		m.metrics.OutboundRequest("stream_session", "transport_error")
		return nil, fmt.Errorf("start peer stream session %s: %w", peerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode >= 500:
			m.metrics.OutboundRequest("stream_session", "5xx")
		default:
			m.metrics.OutboundRequest("stream_session", "4xx")
		}
		return nil, decodeRemoteError(resp)
	}
	m.metrics.OutboundRequest("stream_session", "ok")
	var out PeerStreamSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return &out, nil
}

// ProxyPeerItemPoster obtiene el poster de un item via GET autenticado.
// El remoto re-verifica CanBrowse; perder el share bloquea la lectura.
// Caller DEBE hacer defer resp.Body.Close().
func (m *Manager) ProxyPeerItemPoster(ctx context.Context, peerID, itemID string) (*http.Response, error) {
	return m.ProxyPeerStreamRequest(ctx, peerID, fmt.Sprintf("/api/v1/peer/items/%s/poster", itemID))
}

// ProxyPeerStreamRequest hace GET autenticado y devuelve la respuesta live.
// Callers DEBEN hacer defer resp.Body.Close(). Usado para proxy HLS.
// Si recibe 401/403, reintenta una vez con JWT fresco (race de TTL
// entre relojes). La sesion va por session_id, no por JWT, asi que
// un token nuevo retoma la misma sesion.
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

	resp, err := m.proxyPeerAttempt(ctx, peerID, url)
	if err != nil {
		m.metrics.OutboundRequest("stream_proxy", "transport_error")
		return nil, fmt.Errorf("proxy peer stream %s: %w", peerID, err)
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		m.recordProxyOutcome(resp.StatusCode)
		return resp, nil
	}

	// Drenar para devolver el socket al pool idle (net/http).
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()

	retried, retryErr := m.proxyPeerAttempt(ctx, peerID, url)
	if retryErr != nil {
		m.metrics.OutboundRequest("stream_proxy", "transport_error")
		return nil, fmt.Errorf("proxy peer stream %s (after auth refresh): %w", peerID, retryErr)
	}
	m.recordProxyOutcome(retried.StatusCode)
	return retried, nil
}

// recordProxyOutcome maps an HTTP status from a proxy attempt to the
// outbound-request counter labels.
func (m *Manager) recordProxyOutcome(status int) {
	switch {
	case status >= 500:
		m.metrics.OutboundRequest("stream_proxy", "5xx")
	case status >= 400:
		m.metrics.OutboundRequest("stream_proxy", "4xx")
	default:
		m.metrics.OutboundRequest("stream_proxy", "ok")
	}
}

// proxyPeerAttempt issues a single authenticated GET and returns the
// raw response; reused by ProxyPeerStreamRequest for both the initial
// attempt and the post-401/403 refresh.
func (m *Manager) proxyPeerAttempt(ctx context.Context, peerID, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	tok, err := m.IssuePeerToken(ctx, peerID)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return m.httpClt.Do(req)
}

