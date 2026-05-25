package federation

// Flujo "Steam-style" de pairing requests (sin invite). Protocolo:
// 1) A probea B, POSTea request. 2) B acepta/declina, POSTea callback.
// 3) A valida firma Ed25519 del callback. 4) Cancel/expiry.
// MITM del step 1 se mitiga comparando huella OOB antes del accept.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// PairingRequestTTL: 7 dias. Ventana razonable para que el admin
// remoto compare huella y acepte.
const PairingRequestTTL = 7 * 24 * time.Hour

// Eventos publicados al bus para notificaciones admin.
const (
	EventPairingRequestReceived event.Type = "federation.pairing_request_received"
	EventPairingRequestAccepted event.Type = "federation.pairing_request_accepted"
	EventPairingRequestDeclined event.Type = "federation.pairing_request_declined"
)

// pairingRequestBody es el wire format del POST inicial A -> B.
type pairingRequestBody struct {
	RequestID    string      `json:"request_id"`
	RequestToken string      `json:"request_token"`
	Requester    *ServerInfo `json:"requester"`
}

// pairingCallbackBody es el wire format del POST de respuesta B -> A.
type pairingCallbackBody struct {
	Outcome      string      `json:"outcome"`       // "accepted" | "declined"
	RequestToken string      `json:"request_token"` // el mismo que viajo en step 1
	Accepter     *ServerInfo `json:"accepter"`      // info actualizada de B
	Signature    string      `json:"signature"`     // base64(Ed25519(B.priv, signedMessage))
}

// signedMessage para el callback. Evita reply attack cross-pair.
func pairingCallbackSignedMessage(requestID, outcome string, aPubkey []byte) []byte {
	buf := bytes.Buffer{}
	buf.WriteString("hubplay-federation-pairing-callback-v1\n")
	buf.WriteString("request_id=")
	buf.WriteString(requestID)
	buf.WriteByte('\n')
	buf.WriteString("outcome=")
	buf.WriteString(outcome)
	buf.WriteByte('\n')
	buf.WriteString("recipient_pubkey=")
	buf.Write(aPubkey)
	return buf.Bytes()
}

// pairingCancelBody es el wire format del POST cancel A -> B.
type pairingCancelBody struct {
	RequestToken string `json:"request_token"`
}

// ────────────────────────────────────────────────────────────────────
// Outbound (A side): send request, handle callback from B
// ────────────────────────────────────────────────────────────────────

// SendPairingRequest envia peticion a `baseURL`. Probea, genera token,
// POSTea, y persiste OUTGOING pending.
func (m *Manager) SendPairingRequest(ctx context.Context, baseURL, userID string) (*PendingRequest, error) {
	baseURL = trimSlash(baseURL)
	if err := validatePeerURL(baseURL); err != nil {
		return nil, err
	}
	// Probear B para obtener su ServerInfo y pinear pubkey.
	theirs, err := m.ProbePeer(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	// Conflicto si ya estamos paired.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, theirs.ServerUUID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}
	// Idempotencia: si ya hay outgoing pending, devolverla.
	if existing, found, err := m.repo.GetActivePendingRequestByPeer(ctx, PendingDirectionOutgoing, theirs.ServerUUID); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}

	// Generar secreto compartido + UUID.
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	requestID := uuid.NewString()
	now := m.clock.Now()
	pending := &PendingRequest{
		ID:                 requestID,
		Direction:          PendingDirectionOutgoing,
		PeerServerUUID:     theirs.ServerUUID,
		PeerName:           theirs.Name,
		PeerBaseURL:        baseURL,
		PeerPublicKey:      theirs.PublicKey,
		PeerAvatarColor:    theirs.AvatarColor,
		PeerAvatarImageURL: theirs.AvatarImageURL,
		RequestToken:       token,
		CreatedAt:          now,
		ExpiresAt:          now.Add(PairingRequestTTL),
		Status:             PendingStatusPending,
	}

	// POST a B. Si falla, no persistimos local.
	ours := m.PublicServerInfo()
	body := pairingRequestBody{
		RequestID:    requestID,
		RequestToken: token,
		Requester:    ours,
	}
	if err := m.postPairingRequest(ctx, baseURL, body); err != nil {
		return nil, err
	}

	// Persistir local. Si falla, B tiene huerfano que expira solo.
	if err := m.repo.InsertPendingRequest(ctx, pending); err != nil {
		return nil, err
	}
	return pending, nil
}

// HandlePairingCallback consume el callback de B. Verifica firma +
// token, y aplica la transicion (crear Peer o marcar declined).
func (m *Manager) HandlePairingCallback(ctx context.Context, requestID, outcome, token string, accepter *ServerInfo, signature []byte) error {
	pending, err := m.repo.GetPendingRequestByID(ctx, requestID)
	if err != nil {
		return err
	}
	if pending.Direction != PendingDirectionOutgoing {
		return fmt.Errorf("federation: callback only valid for outgoing requests")
	}
	if pending.Status != PendingStatusPending {
		return fmt.Errorf("federation: callback for request in terminal state (%s)", pending.Status)
	}
	if pending.RequestToken != token {
		return fmt.Errorf("federation: callback token mismatch")
	}
	if accepter == nil || accepter.ServerUUID != pending.PeerServerUUID {
		return fmt.Errorf("federation: callback server_uuid mismatch")
	}
	// Verificar firma con pubkey pineado. Rechazar si difiere.
	if !bytes.Equal(accepter.PublicKey, pending.PeerPublicKey) {
		return fmt.Errorf("federation: callback pubkey doesn't match pinned key")
	}
	signed := pairingCallbackSignedMessage(requestID, outcome, m.identity.Current().PublicKey)
	if !VerifyPeer(pending.PeerPublicKey, signed, signature) {
		return fmt.Errorf("federation: callback signature invalid")
	}

	now := m.clock.Now()
	switch outcome {
	case "accepted":
		if err := m.repo.MarkPendingRequestResponded(ctx, requestID, PendingStatusAccepted, "", now); err != nil {
			return err
		}
		// Crear Peer paired con branding actualizado del accepter.
		peer := &Peer{
			ID:                 uuid.NewString(),
			ServerUUID:         accepter.ServerUUID,
			Name:               accepter.Name,
			BaseURL:            pending.PeerBaseURL,
			PublicKey:          accepter.PublicKey,
			Status:             PeerPaired,
			CreatedAt:          now,
			PairedAt:           &now,
			AvatarColor:        accepter.AvatarColor,
			AvatarImageURL:     accepter.AvatarImageURL,
		}
		if err := m.repo.InsertPeer(ctx, peer); err != nil {
			return fmt.Errorf("federation: persist peer on accept: %w", err)
		}
		if err := m.refreshPeerCache(ctx); err != nil {
			m.logger.Warn("federation: peer cache refresh after pairing-accept failed", "err", err)
		}
		m.publish(EventPeerLinked, map[string]any{
			"peer_id":     peer.ID,
			"server_uuid": peer.ServerUUID,
			"name":        peer.Name,
			"fingerprint": peer.Fingerprint(),
		})
		m.publish(EventPairingRequestAccepted, map[string]any{
			"request_id":  requestID,
			"peer_id":     peer.ID,
			"peer_name":   peer.Name,
			"server_uuid": peer.ServerUUID,
		})
	case "declined":
		if err := m.repo.MarkPendingRequestResponded(ctx, requestID, PendingStatusDeclined, "", now); err != nil {
			return err
		}
		m.publish(EventPairingRequestDeclined, map[string]any{
			"request_id": requestID,
			"peer_name":  pending.PeerName,
		})
	default:
		return fmt.Errorf("federation: unknown callback outcome %q", outcome)
	}
	return nil
}

// CancelPairingRequest marca cancelled local y notifica a B (best-effort).
func (m *Manager) CancelPairingRequest(ctx context.Context, requestID, userID string) error {
	pending, err := m.repo.GetPendingRequestByID(ctx, requestID)
	if err != nil {
		return err
	}
	if pending.Direction != PendingDirectionOutgoing {
		return fmt.Errorf("federation: only outgoing requests can be cancelled by sender")
	}
	if pending.Status != PendingStatusPending {
		return fmt.Errorf("federation: request already %s", pending.Status)
	}
	if err := m.repo.MarkPendingRequestResponded(ctx, requestID, PendingStatusCancelled, userID, m.clock.Now()); err != nil {
		return err
	}
	// Best-effort: notificar a B. Si falla por red, no hay
	// problema - el barrido de expiry de B (~7 dias) limpiara.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), m.cfg.HTTPTimeout)
		defer cancel()
		if err := m.postPairingCancel(bgCtx, pending); err != nil {
			m.logger.Warn("federation: pairing-cancel notification failed",
				"request_id", requestID, "peer_url", pending.PeerBaseURL, "err", err)
		}
	}()
	return nil
}

// ────────────────────────────────────────────────────────────────────
// Inbound (B side): receive request, accept/decline
// ────────────────────────────────────────────────────────────────────

// HandleIncomingPairingRequest valida y persiste como INCOMING pending.
// Idempotente sobre (direction, peer_server_uuid).
func (m *Manager) HandleIncomingPairingRequest(ctx context.Context, requestID, requestToken string, requester *ServerInfo) (*PendingRequest, error) {
	if requester == nil || requester.ServerUUID == "" || len(requester.PublicKey) == 0 {
		return nil, domain.NewValidationError(map[string]string{"requester": "missing or malformed"})
	}
	if err := validatePeerURL(requester.AdvertisedURL); err != nil {
		return nil, err
	}
	if requestID == "" || requestToken == "" {
		return nil, domain.NewValidationError(map[string]string{"request_id": "id+token required"})
	}
	// Toggle admin: rechazar si peticiones deshabilitadas.
	if !m.AcceptingPairingRequests(ctx) {
		return nil, domain.ErrPairingRequestsDisabled
	}
	// Cap defensivo contra flood de incoming pending.
	if cap := m.cfg.MaxIncomingPendingRequests; cap > 0 {
		count, err := m.repo.CountUnreadIncomingPendingRequests(ctx)
		if err == nil && count >= cap {
			return nil, domain.ErrPairingRequestQuotaExceeded
		}
	}
	// Conflicto si ya paired con este server_uuid.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, requester.ServerUUID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}
	// Idempotencia: devolver incoming existente.
	if existing, found, err := m.repo.GetActivePendingRequestByPeer(ctx, PendingDirectionIncoming, requester.ServerUUID); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}

	now := m.clock.Now()
	pending := &PendingRequest{
		ID:                 requestID,
		Direction:          PendingDirectionIncoming,
		PeerServerUUID:     requester.ServerUUID,
		PeerName:           requester.Name,
		PeerBaseURL:        requester.AdvertisedURL,
		PeerPublicKey:      requester.PublicKey,
		PeerAvatarColor:    requester.AvatarColor,
		PeerAvatarImageURL: requester.AvatarImageURL,
		RequestToken:       requestToken,
		CreatedAt:          now,
		ExpiresAt:          now.Add(PairingRequestTTL),
		Status:             PendingStatusPending,
	}
	if err := m.repo.InsertPendingRequest(ctx, pending); err != nil {
		return nil, err
	}
	m.publish(EventPairingRequestReceived, map[string]any{
		"request_id":   requestID,
		"peer_name":    requester.Name,
		"server_uuid":  requester.ServerUUID,
		"fingerprint":  Fingerprint(requester.PublicKey),
	})
	return pending, nil
}

// AcceptPairingRequest finaliza una incoming pending: crea Peer paired
// + notifica callback al remitente (best-effort).
func (m *Manager) AcceptPairingRequest(ctx context.Context, requestID, userID string) (*Peer, error) {
	return m.resolveIncoming(ctx, requestID, userID, "accepted")
}

func (m *Manager) DeclinePairingRequest(ctx context.Context, requestID, userID string) error {
	_, err := m.resolveIncoming(ctx, requestID, userID, "declined")
	return err
}

func (m *Manager) resolveIncoming(ctx context.Context, requestID, userID, outcome string) (*Peer, error) {
	pending, err := m.repo.GetPendingRequestByID(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if pending.Direction != PendingDirectionIncoming {
		return nil, fmt.Errorf("federation: only incoming requests can be accepted/declined")
	}
	if pending.Status != PendingStatusPending {
		return nil, fmt.Errorf("federation: request already %s", pending.Status)
	}
	if !pending.IsActive(m.clock.Now()) {
		// Expirada entre GET y marcado.
		return nil, domain.ErrNotFound
	}

	status := PendingStatusAccepted
	if outcome == "declined" {
		status = PendingStatusDeclined
	}
	now := m.clock.Now()
	if err := m.repo.MarkPendingRequestResponded(ctx, requestID, status, userID, now); err != nil {
		return nil, err
	}

	var peer *Peer
	if outcome == "accepted" {
		peer = &Peer{
			ID:                 uuid.NewString(),
			ServerUUID:         pending.PeerServerUUID,
			Name:               pending.PeerName,
			BaseURL:            pending.PeerBaseURL,
			PublicKey:          pending.PeerPublicKey,
			Status:             PeerPaired,
			CreatedAt:          now,
			PairedAt:           &now,
			AvatarColor:        pending.PeerAvatarColor,
			AvatarImageURL:     pending.PeerAvatarImageURL,
		}
		if err := m.repo.InsertPeer(ctx, peer); err != nil {
			return nil, fmt.Errorf("federation: persist peer on accept: %w", err)
		}
		if err := m.refreshPeerCache(ctx); err != nil {
			m.logger.Warn("federation: peer cache refresh after pairing-accept failed", "err", err)
		}
		m.publish(EventPeerLinked, map[string]any{
			"peer_id":     peer.ID,
			"server_uuid": peer.ServerUUID,
			"name":        peer.Name,
			"fingerprint": peer.Fingerprint(),
		})
	}

	// Best-effort callback. Si falla, A ve pendiente hasta expiry.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), m.cfg.HTTPTimeout)
		defer cancel()
		if err := m.postPairingCallback(bgCtx, pending, outcome); err != nil {
			m.logger.Warn("federation: pairing-callback notification failed",
				"request_id", requestID, "outcome", outcome, "err", err)
		}
	}()

	return peer, nil
}

// ListPendingRequests devuelve ambas direcciones, reciente primero.
func (m *Manager) ListPendingRequests(ctx context.Context, limit int) ([]*PendingRequest, error) {
	return m.repo.ListPendingRequests(ctx, limit)
}

// GetPendingRequest devuelve una peticion por ID.
func (m *Manager) GetPendingRequest(ctx context.Context, id string) (*PendingRequest, error) {
	return m.repo.GetPendingRequestByID(ctx, id)
}

// CountIncomingPending para el badge del panel admin.
func (m *Manager) CountIncomingPending(ctx context.Context) (int, error) {
	return m.repo.CountUnreadIncomingPendingRequests(ctx)
}

// SweepExpiredPairingRequests llamado por el job periodico de main.go.
func (m *Manager) SweepExpiredPairingRequests(ctx context.Context) (int, error) {
	return m.repo.ExpirePendingRequests(ctx, m.clock.Now())
}

// CancelIncomingPairingRequest marca cancelled (lado B). No notifica a A.
func (m *Manager) CancelIncomingPairingRequest(ctx context.Context, requestID string) error {
	return m.repo.MarkPendingRequestResponded(ctx, requestID, PendingStatusCancelled, "", m.clock.Now())
}

// ────────────────────────────────────────────────────────────────────
// HTTP helpers (outbound POSTs)
// ────────────────────────────────────────────────────────────────────

func (m *Manager) postPairingRequest(ctx context.Context, baseURL string, body pairingRequestBody) error {
	url, err := joinBaseURL(baseURL, "/api/v1/federation/pairing-requests")
	if err != nil {
		return err
	}
	return m.postJSON(ctx, url, body, "pairing-request")
}

func (m *Manager) postPairingCallback(ctx context.Context, pending *PendingRequest, outcome string) error {
	url, err := joinBaseURL(pending.PeerBaseURL, fmt.Sprintf("/api/v1/federation/pairing-requests/%s/callback", pending.ID))
	if err != nil {
		return err
	}
	ours := m.PublicServerInfo()
	// Firmar con nuestra privkey; A verifica con el pubkey pineado.
	signed := pairingCallbackSignedMessage(pending.ID, outcome, pending.PeerPublicKey)
	sig := m.identity.Current().Sign(signed)
	body := pairingCallbackBody{
		Outcome:      outcome,
		RequestToken: pending.RequestToken,
		Accepter:     ours,
		Signature:    encodeBase64(sig),
	}
	return m.postJSON(ctx, url, body, "pairing-callback")
}

func (m *Manager) postPairingCancel(ctx context.Context, pending *PendingRequest) error {
	url, err := joinBaseURL(pending.PeerBaseURL, fmt.Sprintf("/api/v1/federation/pairing-requests/%s/cancel", pending.ID))
	if err != nil {
		return err
	}
	body := pairingCancelBody{RequestToken: pending.RequestToken}
	return m.postJSON(ctx, url, body, "pairing-cancel")
}

func (m *Manager) postJSON(ctx context.Context, url string, body any, label string) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpClt.Do(req)
	if err != nil {
		return fmt.Errorf("federation: %s POST %s: %w", label, url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("federation: %s POST %s: status %d: %s", label, url, resp.StatusCode, string(preview))
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────

// randomToken genera 16 bytes aleatorios en hex. 128 bits.
func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func encodeBase64(b []byte) string {
	// base64.StdEncoding para coincidir con EncodePublicKey.
	return EncodePublicKey(b)
}

// ErrPairingRequestNotFound sentinel para conveniencia en handlers.
var ErrPairingRequestNotFound = errors.New("federation: pairing request not found")
