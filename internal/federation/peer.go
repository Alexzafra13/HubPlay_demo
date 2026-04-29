package federation

import (
	"crypto/ed25519"
	"time"
)

// PeerStatus is the lifecycle of a peer record. Transitions are
// strictly forward: pending → paired → revoked. A revoked peer is
// terminal — to re-pair you start a fresh handshake.
type PeerStatus string

const (
	// PeerPending: invite generated locally, not yet completed by remote.
	PeerPending PeerStatus = "pending"
	// PeerPaired: handshake complete; peer is fully linked.
	PeerPaired PeerStatus = "paired"
	// PeerRevoked: admin has revoked; all subsequent peer JWTs are rejected.
	PeerRevoked PeerStatus = "revoked"
)

// Peer is a known remote HubPlay server.
//
// PublicKey is the pinned Ed25519 pubkey we exchanged during the
// handshake; every inbound request from this peer must carry a JWT
// signed by the matching private key. A pubkey mismatch fires
// ErrPeerKeyMismatch and an audit event.
//
// Status governs whether the peer can do anything. Revoked is
// terminal — the row stays for audit but the row never participates
// in federation flows again.
type Peer struct {
	ID                 string
	ServerUUID         string
	Name               string
	BaseURL            string
	PublicKey          ed25519.PublicKey
	Status             PeerStatus
	CreatedAt          time.Time
	PairedAt           *time.Time
	LastSeenAt         *time.Time
	LastSeenStatusCode *int
	RevokedAt          *time.Time
}

// Fingerprint returns the same SSH-style hex fingerprint of the peer's
// pinned pubkey that the admin saw at handshake time. Useful for
// rendering peer cards in the admin UI and for audit log entries.
func (p *Peer) Fingerprint() string {
	return Fingerprint(p.PublicKey)
}

// IsActive reports whether this peer can participate in federation
// flows right now. A pending peer cannot — handshake hasn't completed.
// A revoked peer cannot — terminal.
func (p *Peer) IsActive() bool {
	return p.Status == PeerPaired
}

// Invite is a single-use code minted by the local admin so a remote
// admin can complete the handshake on their side. The code is the only
// secret in the invite — neither side's pubkey is in the invite
// payload, because the pubkeys are exchanged DURING the handshake (the
// invite carries only the bearer-token contract).
type Invite struct {
	ID               string
	Code             string
	CreatedByUserID  string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	AcceptedByPeerID *string
	AcceptedAt       *time.Time
}

// IsUsable reports whether an invite can still complete a handshake at
// the given moment. A used invite (AcceptedAt non-nil) returns false —
// invites are single-use by design.
func (i *Invite) IsUsable(now time.Time) bool {
	if i.AcceptedAt != nil {
		return false
	}
	return now.Before(i.ExpiresAt)
}

// ServerInfo is the public-facing identity surface returned by the
// /federation/info endpoint. It carries everything the receiving
// admin needs to confirm a handshake target out-of-band: name, URL,
// version, supported scopes, and the fingerprint they should compare
// to what the inviting admin reads to them.
//
// pubkey is bytes-on-the-wire; the JSON layer base64-encodes for
// transport.
type ServerInfo struct {
	ServerUUID        string   `json:"server_uuid"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	PublicKey         []byte   `json:"public_key"`
	PubkeyFingerprint string   `json:"pubkey_fingerprint"`
	PubkeyWords       []string `json:"pubkey_words"`
	SupportedScopes   []string `json:"supported_scopes"`
	AdvertisedURL     string   `json:"advertised_url"`
	AdminContact      string   `json:"admin_contact,omitempty"`
}
