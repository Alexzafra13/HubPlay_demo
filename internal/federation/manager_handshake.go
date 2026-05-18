package federation

// Manager methods that own the peer pairing handshake — outbound
// (we accept a remote invite) and inbound (a remote pasted our
// invite into their UI). Lifted out of manager.go so the 1100-line
// monolith doesn't hide what's a self-contained protocol.

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

// ProbePeer fetches the remote's /federation/info so the local admin
// can see the fingerprint before committing to handshake. Read-only;
// no state mutation on either side.
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
	// Recompute fingerprint locally — never trust the wire's claim.
	// If they disagree, the wire fingerprint is misleading and the
	// admin needs to see the locally-derived one.
	info.PubkeyFingerprint = Fingerprint(info.PublicKey)
	info.PubkeyWords = FingerprintWords(info.PublicKey)
	return &info, nil
}

// AcceptInvite completes the handshake from our side: we POST to the
// remote's /peer/handshake with their invite code + our ServerInfo;
// the remote validates the code, persists us as a peer, and returns
// their ServerInfo. We persist them as a peer too. Both sides end
// with status='paired' and pinned pubkeys.
//
// The remote URL must match what the admin saw in ProbePeer — the
// admin should have visually confirmed the fingerprint already.
//
// fallbackAdvertisedURL is the URL we send to the remote as our own
// reachable address, USED ONLY IF cfg.AdvertisedURL is empty. The
// admin handler derives this from the admin's session request so a
// fresh deployment that hasn't set HUBPLAY_SERVER_BASE_URL still
// pairs successfully — plug-and-play.
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

	// Persist them as a paired peer. Capturamos el branding (color +
	// URL de la foto) que el remoto publica en su ServerInfo para
	// que PeersTable lo pinte sin tener que volver a pegarlo cada
	// vez. Si el remoto los cambia luego, el admin pulsa el boton
	// "Actualizar" y se refresca via Manager.RefreshPeerBranding.
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

// RefreshPeerBranding re-probea /federation/info del peer y persiste
// los campos visuales (nombre + color + URL de la foto) en BD. El
// admin lo invoca desde el boton "Actualizar" de PeersTable cuando
// el remoto ha cambiado su marca y queremos que la nuestra refleje
// el cambio sin tener que revocar + re-pair.
//
// El pubkey + server_uuid + base_url NO se actualizan — esos son
// la identidad criptografica del peer y solo se establecen via
// handshake. Si el remoto rota su keypair (Phase 2+) hace falta
// un flow distinto.
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
	// Sanity check: el server_uuid del peer probed tiene que coincidir
	// con el que tenemos pinneado. Si no, alguien ha tomado control de
	// la URL o el peer ha rotado su identidad — en ambos casos no
	// queremos sobreescribir su branding silenciosamente.
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

// HandleInboundHandshake validates the code, persists the remote as a
// paired peer, marks the invite consumed, and returns our own
// ServerInfo so the caller can persist us on their side. Atomic in
// spirit — failures partway through leave the invite consumable for
// another retry, since we update it last.
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
	// SSRF gate: a hostile peer with a valid invite must not be able
	// to advertise a URL pointing at our localhost or a link-local
	// address. We pin remote.AdvertisedURL onto peer.BaseURL below;
	// every future outbound call uses it, so the validation has to
	// happen before persistence.
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

	// If we've already paired with this server_uuid (e.g. retry of a
	// previous handshake), surface as a conflict so the local admin
	// can decide whether to revoke + re-pair.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, remote.ServerUUID); err == nil && existing != nil {
		return nil, nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}

	// Mismo motivo que en AcceptInvite: capturamos el branding del
	// remoto que recibimos en su ServerInfo para no perderlo. Si
	// luego lo cambia, se refresca desde PeersTable manualmente.
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
		// If marking the invite used failed, we already inserted the
		// peer — log loudly; the admin can revoke + clean up. The
		// alternative (rolling back the peer insert) would require a
		// transaction across two repo methods which the rest of the
		// codebase doesn't currently do.
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
