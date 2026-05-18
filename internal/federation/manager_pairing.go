package federation

// Manager methods que implementan el flujo "Steam-style" de
// pairing requests (sin codigo de invitacion). El flujo legacy
// (Invite + AcceptInvite + /peer/handshake) sigue intacto en
// manager_handshake.go - este vive en paralelo para que cada
// admin elija el que prefiera.
//
// Protocolo de 4 pasos:
//
//   1. A.SendPairingRequest(B_url): A probea B/federation/info para
//      obtener su ServerInfo (sobre todo el pubkey, que pinea), y
//      POSTea B/federation/pairing-requests con su propio ServerInfo
//      + un request_token nuevo. A persiste OUTGOING pending.
//      B persiste INCOMING pending y notifica al inbox de sus admins.
//
//   2. B.AcceptPairingRequest(request_id) o B.DeclinePairingRequest:
//      admin B (tras comparar huella OOB con A) lo acepta. B marca
//      su pending como accepted/declined. Si acepta, crea Peer paired.
//      Luego POSTea A/federation/pairing-requests/{id}/callback con
//      su ServerInfo + outcome firmado con B.privkey.
//
//   3. A.HandlePairingCallback: A valida la firma con el pubkey que
//      pineo en step 1. Si accepted, marca su outgoing como accepted +
//      crea Peer paired. Si declined, marca como declined.
//
//   4. Cancel / expiry: A puede cancelar su outgoing en cualquier
//      momento (best-effort POST a B/federation/pairing-requests/{id}/
//      cancel para limpiar el inbox de B). El job periodico mueve
//      pendientes con expires_at < ahora a 'expired'.
//
// MITM: la unica defensa criptografica fuerte es la firma Ed25519
// del callback en step 3 (verificada con el pubkey pineado en step 1).
// MITM del wire en step 1 NO esta cubierto - de ahi que la huella
// se compare OOB antes del accept en step 2, igual que en el flow
// legacy.

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

// PairingRequestTTL es cuanto vive una peticion antes de expirar.
// 7 dias: el admin invitador puede pulsar "enviar" y olvidarse; el
// invitado tiene una semana razonable para verle la huella, comparar
// y aceptar sin que la peticion se vaya. Misma ventana que muchas
// apps tipo Slack/Discord para invitaciones a workspaces.
const PairingRequestTTL = 7 * 24 * time.Hour

// Eventos publicados al EventBus. Los subs (notifications service)
// los consumen para crear notificaciones en el inbox del usuario.
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

// signedMessage para el callback. Cubrir request_id + outcome +
// pubkey de A en bytes evita reply attack cross-pair.
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

// SendPairingRequest envia una peticion al servidor en `baseURL`.
// Probea su /federation/info para pinear su identidad, genera un
// token, POSTea el body, y persiste como OUTGOING pending. El admin
// puede entonces seguir el estado en su panel "Peticiones enviadas".
//
// Errores:
//   - domain.ErrAlreadyExists si ya estamos paired o ya hay una
//     pending con ese server_uuid.
//   - PEER_PROBE_FAILED si no podemos conectar a `baseURL`.
//   - validacion (URL invalida, SSRF) - mismo guard que AcceptInvite.
func (m *Manager) SendPairingRequest(ctx context.Context, baseURL, userID string) (*PendingRequest, error) {
	baseURL = trimSlash(baseURL)
	if err := validatePeerURL(baseURL); err != nil {
		return nil, err
	}
	// Step A: probear B para obtener su ServerInfo (sobre todo el pubkey
	// que vamos a pinear).
	theirs, err := m.ProbePeer(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	// Ya estamos paired? Devolver conflicto explicito.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, theirs.ServerUUID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}
	// Hay ya una outgoing pending con este server_uuid? Devolverla
	// (idempotencia para que pulsar "enviar" dos veces no llene la
	// tabla con duplicados).
	if existing, found, err := m.repo.GetActivePendingRequestByPeer(ctx, PendingDirectionOutgoing, theirs.ServerUUID); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}

	// Genera secreto compartido + UUID de request.
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

	// Step B: POST a B/federation/pairing-requests con nuestro
	// ServerInfo. Si falla NO persistimos local (no hay nada que
	// el admin pueda hacer con una peticion que el remoto no
	// recibio).
	ours := m.PublicServerInfo()
	body := pairingRequestBody{
		RequestID:    requestID,
		RequestToken: token,
		Requester:    ours,
	}
	if err := m.postPairingRequest(ctx, baseURL, body); err != nil {
		return nil, err
	}

	// Step C: persistir local. Si esto falla tras el POST de B,
	// B tiene un huerfano en su inbox - se purgara via expiry.
	if err := m.repo.InsertPendingRequest(ctx, pending); err != nil {
		return nil, err
	}
	return pending, nil
}

// HandlePairingCallback consume el callback que B nos envia tras
// accept/decline. Verifica firma + token + estado, y aplica la
// transicion local (crear Peer paired si accept; marcar declined
// si decline). Tambien emite eventos al bus para alimentar el
// inbox de notificaciones del admin local.
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
	// Verificar firma: usamos el pubkey que pineamos en SendPairingRequest.
	// Si la firma valida pero el accepter trae un pubkey distinto al
	// pineado, lo rechazamos como sustituto de identidad.
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
		// Crear Peer paired - capturamos el branding actualizado del
		// accepter (puede haber cambiado entre nuestro probe inicial y
		// el accept).
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

// CancelPairingRequest la marca cancelled local + (best-effort)
// notifica a B para que limpie su inbox. Si B no responde, el
// barrido de expiry hara la limpieza eventual.
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

// HandleIncomingPairingRequest valida el body del POST de A y lo
// persiste como INCOMING pending. Tambien emite evento para que el
// notification service notifique a todos los admins.
//
// Idempotente sobre (direction, peer_server_uuid): si A reenvia
// mientras la primera sigue pending, devolvemos la misma id sin
// duplicar (el indice unico parcial lo bloquearia igualmente).
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
	// Si ya estamos paired con este server_uuid, conflicto.
	if existing, err := m.repo.GetPeerByServerUUID(ctx, requester.ServerUUID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: server_uuid already paired", domain.ErrAlreadyExists)
	}
	// Idempotencia: si ya hay una incoming pendiente con este server_uuid,
	// devolverla.
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
// + (best-effort) notifica callback al remitente. El admin debe haber
// comparado la huella OOB antes - este metodo confia en eso.
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
		// Edge: expirada entre la GET y el marcado.
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

	// Best-effort callback al remitente. Si falla por red, A vera la
	// peticion como pendiente en su panel hasta que expire (~7 dias).
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

// ListPendingRequests devuelve ambas direcciones, mas reciente primero.
func (m *Manager) ListPendingRequests(ctx context.Context, limit int) ([]*PendingRequest, error) {
	return m.repo.ListPendingRequests(ctx, limit)
}

// GetPendingRequest devuelve una sola.
func (m *Manager) GetPendingRequest(ctx context.Context, id string) (*PendingRequest, error) {
	return m.repo.GetPendingRequestByID(ctx, id)
}

// CountIncomingPending para el badge admin.
func (m *Manager) CountIncomingPending(ctx context.Context) (int, error) {
	return m.repo.CountUnreadIncomingPendingRequests(ctx)
}

// SweepExpiredPairingRequests es lo que llama el job periodico
// (lo cableamos en main.go).
func (m *Manager) SweepExpiredPairingRequests(ctx context.Context) (int, error) {
	return m.repo.ExpirePendingRequests(ctx, m.clock.Now())
}

// CancelIncomingPairingRequest la marca cancelled (lado B, cuando
// A nos avisa que cancela). No notifica a A - el cancel viene
// precisamente de A.
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
	// Firmamos el callback con nuestra privkey. El receptor (A) verifica
	// con el pubkey que pineo en SendPairingRequest.
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

// randomToken genera 16 bytes de aleatoriedad y los devuelve hex.
// 128 bits = mas que suficiente para que un atacante no pueda
// adivinar el token de un callback en una ventana razonable.
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
	// Usamos base64.StdEncoding aquí para coincidir con
	// EncodePublicKey/DecodePublicKey del paquete.
	return EncodePublicKey(b)
}

// ErrPairingRequestNotFound es un sentinel para conveniencia en
// handlers (evita usar errors.Is con un literal de domain.ErrNotFound
// disperso). Aliased.
var ErrPairingRequestNotFound = errors.New("federation: pairing request not found")
