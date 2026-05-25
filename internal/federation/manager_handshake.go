package federation

// Metodos de Manager para el handshake de pairing: outbound e inbound.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"hubplay/internal/domain"
)

// handshakeRequest is the POST body of /peer/handshake.
type handshakeRequest struct {
	Code       string      `json:"code"`
	RemoteInfo *ServerInfo `json:"remote_info"`
}

// ────────────────────────────────────────────────────────────────────
// Outbound handshake (we received an invite from the remote admin)
// ────────────────────────────────────────────────────────────────────

// ProbePeer obtiene /federation/info del remoto. Solo lectura.
func (m *Manager) ProbePeer(ctx context.Context, baseURL string) (*ServerInfo, error) {
	url, err := joinBaseURL(baseURL, "/api/v1/federation/info")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, fmt.Errorf("federation: probe %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return nil, fmt.Errorf("federation: probe %s: status %d: %s", baseURL, resp.StatusCode, body)
	}
	var info ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("federation: decode info from %s: %w", baseURL, err)
	}
	if len(info.PublicKey) == 0 || info.ServerUUID == "" {
		return nil, fmt.Errorf("federation: probe %s: malformed info", baseURL)
	}
	// Recomputar fingerprint localmente — nunca confiar en el wire.
	info.PubkeyFingerprint = Fingerprint(info.PublicKey)
	info.PubkeyWords = FingerprintWords(info.PublicKey)
	return &info, nil
}

// AcceptInvite completa el handshake: POST al remoto con invite code +
// nuestro ServerInfo. Ambos lados terminan paired con pubkeys pineados.
// fallbackAdvertisedURL se usa solo si cfg.AdvertisedURL esta vacio.
func (m *Manager) AcceptInvite(ctx context.Context, baseURL, code, fallbackAdvertisedURL string) (out *Peer, err error) {
	start := m.clock.Now()
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		m.metrics.HandshakeDuration("outbound", outcome, m.clock.Now().Sub(start).Seconds())
	}()

	if err := ValidateCodeFormat(code); err != nil {
		return nil, err
	}
	if err := validatePeerURL(baseURL); err != nil {
		return nil, err
	}
	canonical := CanonicalCode(code)

	url, err := joinBaseURL(baseURL, "/api/v1/peer/handshake")
	if err != nil {
		return nil, err
	}
	ours := m.PublicServerInfo()
	if ours.AdvertisedURL == "" && fallbackAdvertisedURL != "" {
		ours.AdvertisedURL = fallbackAdvertisedURL
	}

	body, err := json.Marshal(handshakeRequest{
		Code:       canonical,
		RemoteInfo: ours,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClt.Do(req)
	if err != nil {
		return nil, fmt.Errorf("federation: handshake %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("federation: handshake %s: status %d: %s", baseURL, resp.StatusCode, raw)
	}
	var theirs ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&theirs); err != nil {
		return nil, fmt.Errorf("federation: decode handshake response: %w", err)
	}
	if len(theirs.PublicKey) == 0 || theirs.ServerUUID == "" {
		return nil, fmt.Errorf("federation: handshake %s: malformed response", baseURL)
	}

	// Persistir como peer paired. Capturamos branding del ServerInfo.
	now := m.clock.Now()
	peer := &Peer{
		ID:             uuid.NewString(),
		ServerUUID:     theirs.ServerUUID,
		Name:           theirs.Name,
		BaseURL:        baseURL,
		PublicKey:      theirs.PublicKey,
		Status:         PeerPaired,
		CreatedAt:      now,
		PairedAt:       &now,
		AvatarColor:    theirs.AvatarColor,
		AvatarImageURL: theirs.AvatarImageURL,
	}
	if err := m.repo.InsertPeer(ctx, peer); err != nil {
		return nil, fmt.Errorf("federation: persist peer: %w", err)
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		m.logger.Warn("federation: peer cache refresh after pairing failed", "err", err)
	}
	m.publish(EventPeerLinked, map[string]any{
		"peer_id":     peer.ID,
		"server_uuid": peer.ServerUUID,
		"name":        peer.Name,
		"fingerprint": peer.Fingerprint(),
	})
	return peer, nil
}

// ────────────────────────────────────────────────────────────────────
// Inbound handshake (a remote admin pasted OUR invite into THEIR UI;
// their server is calling US to complete the link)
// ────────────────────────────────────────────────────────────────────

// RefreshPeerBranding re-probea /federation/info y persiste nombre,
// color y URL de la foto. Pubkey/server_uuid/base_url NO se tocan.
func (m *Manager) RefreshPeerBranding(ctx context.Context, peerID string) (*Peer, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer.Status != PeerPaired {
		return nil, fmt.Errorf("federation: cannot refresh branding of non-paired peer (status=%s)", peer.Status)
	}
	info, err := m.ProbePeer(ctx, peer.BaseURL)
	if err != nil {
		return nil, err
	}
	// Sanity: server_uuid debe coincidir con el pineado.
	if info.ServerUUID != peer.ServerUUID {
		return nil, fmt.Errorf("federation: refreshed info server_uuid mismatch (got %s, expected %s)", info.ServerUUID, peer.ServerUUID)
	}
	if err := m.repo.UpdatePeerBranding(ctx, peerID, info.Name, info.AvatarColor, info.AvatarImageURL); err != nil {
		return nil, err
	}
	peer.Name = info.Name
	peer.AvatarColor = info.AvatarColor
	peer.AvatarImageURL = info.AvatarImageURL
	return peer, nil
}

// HandleInboundHandshake valida el codigo, persiste el peer remoto,
// marca la invite consumida, y devuelve nuestro ServerInfo.
func (m *Manager) HandleInboundHandshake(ctx context.Context, code string, remote *ServerInfo) (outPeer *Peer, outInfo *ServerInfo, err error) {
	start := m.clock.Now()
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		m.metrics.HandshakeDuration("inbound", outcome, m.clock.Now().Sub(start).Seconds())
	}()

	if err := ValidateCodeFormat(code); err != nil {
		return nil, nil, err
	}
	if remote == nil || remote.ServerUUID == "" || len(remote.PublicKey) == 0 {
		return nil, nil, domain.NewValidationError(map[string]string{"remote_info": "missing or malformed"})
	}
	// Gate SSRF: validar URL antes de persistir.
	if err := validatePeerURL(remote.AdvertisedURL); err != nil {
		return nil, nil, err
	}
	canonical := CanonicalCode(code)
	now := m.clock.Now()

	inv, err := m.repo.GetInviteByCode(ctx, canonical)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, domain.ErrInviteNotFound
		}
		return nil, nil, err
	}
	if !inv.IsUsable(now) {
		if inv.AcceptedAt != nil {
			return nil, nil, domain.ErrInviteAlreadyUsed
		}
		return nil, nil, domain.ErrInviteExpired
	}

	// Si ya estamos paired con este server_uuid, conflicto.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, remote.ServerUUID); err == nil && existing != nil {
		return nil, nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}

	// Capturamos branding del remoto.
	peer := &Peer{
		ID:             uuid.NewString(),
		ServerUUID:     remote.ServerUUID,
		Name:           remote.Name,
		BaseURL:        remote.AdvertisedURL,
		PublicKey:      remote.PublicKey,
		Status:         PeerPaired,
		CreatedAt:      now,
		PairedAt:       &now,
		AvatarColor:    remote.AvatarColor,
		AvatarImageURL: remote.AvatarImageURL,
	}
	if err := m.repo.InsertPeer(ctx, peer); err != nil {
		return nil, nil, fmt.Errorf("federation: persist inbound peer: %w", err)
	}
	if err := m.repo.MarkInviteUsed(ctx, inv.ID, peer.ID, now); err != nil {
		// Invite-used fallo tras insertar peer — loguear. Rollback
		// requeriria transaccion cross-repo que no usamos.
		m.logger.Error("federation: invite-used update failed AFTER peer insert",
			"err", err, "invite_id", inv.ID, "peer_id", peer.ID)
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		m.logger.Warn("federation: peer cache refresh after inbound handshake failed", "err", err)
	}
	m.publish(EventInviteUsed, map[string]any{
		"invite_id": inv.ID,
		"peer_id":   peer.ID,
	})
	m.publish(EventPeerLinked, map[string]any{
		"peer_id":     peer.ID,
		"server_uuid": peer.ServerUUID,
		"name":        peer.Name,
		"fingerprint": peer.Fingerprint(),
	})
	return peer, m.PublicServerInfo(), nil
}
