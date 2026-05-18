package federation

import (
	"crypto/ed25519"
	"time"
)

// PendingRequestDirection discrimina si la peticion la enviamos
// nosotros (outgoing) o nos la enviaron a nosotros (incoming).
type PendingRequestDirection string

const (
	PendingDirectionIncoming PendingRequestDirection = "incoming"
	PendingDirectionOutgoing PendingRequestDirection = "outgoing"
)

// PendingRequestStatus es el ciclo de vida. Transiciones:
//   pending -> accepted   (admin acepto - se crea Peer paired)
//   pending -> declined   (admin rechazo)
//   pending -> cancelled  (solo outgoing: el remitente la cancelo)
//   pending -> expired    (job periodico tras TTL)
// Todas son terminales.
type PendingRequestStatus string

const (
	PendingStatusPending   PendingRequestStatus = "pending"
	PendingStatusAccepted  PendingRequestStatus = "accepted"
	PendingStatusDeclined  PendingRequestStatus = "declined"
	PendingStatusCancelled PendingRequestStatus = "cancelled"
	PendingStatusExpired   PendingRequestStatus = "expired"
)

// PendingRequest es una entrada del inbox de pairing requests
// (migration 048). Representa AMBAS direcciones - el campo
// Direction discrimina:
//
//   incoming: alguien nos quiere emparejar. El peer es el remitente.
//             el admin local la ve en su badge + dropdown y acepta /
//             declina.
//
//   outgoing: nosotros queremos emparejar con alguien. El peer es el
//             destinatario. Se queda pending hasta que su admin
//             responda (acepta -> nos llega callback que la marca
//             accepted; declina -> idem; cancela -> propia, llamamos
//             a su lado para liberar el lock).
//
// La pubkey del peer se pinea al CREAR la peticion (al probar A o al
// recibir el body de B) y es lo que usamos para verificar el callback
// post-accept. MITM en el wire del callback queda bloqueado por la
// firma Ed25519.
type PendingRequest struct {
	ID                 string
	Direction          PendingRequestDirection
	PeerServerUUID     string
	PeerName           string
	PeerBaseURL        string
	PeerPublicKey      ed25519.PublicKey
	PeerAvatarColor    string
	PeerAvatarImageURL string
	// RequestToken es un secreto compartido generado por el lado
	// outgoing al crear la peticion. Viaja en el cuerpo de la peticion
	// inicial; cuando B llega para confirmar accept/decline, lo
	// incluye en el callback como prueba "soy quien recibio la
	// peticion original". Defense-in-depth sobre la firma Ed25519.
	RequestToken        string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	Status              PendingRequestStatus
	RespondedAt         *time.Time
	RespondedByUserID   string
}

// IsActive reporta si la peticion puede aceptarse / declinarse
// todavia (pendiente y no expirada al instante `now`).
func (p *PendingRequest) IsActive(now time.Time) bool {
	return p.Status == PendingStatusPending && now.Before(p.ExpiresAt)
}

// Fingerprint del pubkey del peer remoto. Conveniente para que el
// frontend renderice el "compara esto con tu colega" sin tener que
// re-derivarlo.
func (p *PendingRequest) Fingerprint() string {
	return Fingerprint(p.PeerPublicKey)
}

// FingerprintWords idem.
func (p *PendingRequest) FingerprintWords() []string {
	return FingerprintWords(p.PeerPublicKey)
}
