// Package federation implementa peering server-a-server para HubPlay.
package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// Identity es el keypair Ed25519 estable del servidor. Generado en primer
// boot, pineado por cada peer en handshake. AvatarColor + AvatarImagePath
// editables desde admin; expuestos via /federation/info.
type Identity struct {
	ServerUUID      string
	Name            string
	PublicKey       ed25519.PublicKey
	PrivateKey      ed25519.PrivateKey
	CreatedAt       time.Time
	RotatedAt       *time.Time
	AvatarColor     string
	AvatarImagePath string
}

// Fingerprint renderiza el pubkey como 4 grupos hex de 4 chars (estilo SSH).
// SHA-256 del pubkey truncado a 8 bytes: estable y legible en voz alta.
func (i *Identity) Fingerprint() string {
	return Fingerprint(i.PublicKey)
}

// Fingerprint (free function) para renderizar fingerprints de pubkeys
// de peers sin instanciar Identity.
func Fingerprint(pub ed25519.PublicKey) string {
	if len(pub) == 0 {
		return ""
	}
	// SHA-256(pubkey)[:8] → 16 hex chars in 4 groups of 4.
	sum := sha256First8(pub)
	hexed := hex.EncodeToString(sum)
	var sb strings.Builder
	for i := 0; i < len(hexed); i += 4 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(hexed[i : i+4])
	}
	return sb.String()
}

// FingerprintWords returns 6 short pronounceable words derived from the
// fingerprint bytes. Útil para confirmación por voz: "the fingerprint
// starts with aardvark, barbados…". La lista es pequeña (288 entradas)
// y se mapea pseudo-aleatoriamente por byte; resistencia a colisión
// igual al SHA-256 truncado de 8 bytes subyacente.
//
// 6 palabras = 48 bits efectivos de confirmación oral (1 en ~280
// billones), suficiente margen para un canal de voz con confusión
// potencial. Antes eran 4 palabras (~32 bits, 1 en 4 mil millones)
// que ya era seguro contra ataques cazapalabra, pero 6 deja margen
// si el atacante puede lanzar diccionario sobre la transmisión.
func (i *Identity) FingerprintWords() []string {
	return FingerprintWords(i.PublicKey)
}

// FingerprintWords (free-function variant) — see Identity.FingerprintWords.
func FingerprintWords(pub ed25519.PublicKey) []string {
	if len(pub) == 0 {
		return nil
	}
	sum := sha256First8(pub)
	out := make([]string, 6)
	// Usamos los 6 primeros bytes del SHA-256 truncado a 8 (cada uno
	// mapea a una palabra). Quedan 2 bytes sin usar — la entropía
	// criptográfica está en la pubkey entera, no en los bytes
	// individuales, así que truncar más no debilita el protocolo.
	for idx := 0; idx < 6; idx++ {
		out[idx] = phoneticWords[int(sum[idx])%len(phoneticWords)]
	}
	return out
}

// Sign produces a detached Ed25519 signature over msg using this
// server's private key. The peer verifies with VerifyPeer.
func (i *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(i.PrivateKey, msg)
}

// VerifyPeer checks an Ed25519 signature against a peer's pinned pubkey.
// Returns true on success. False covers every failure mode (wrong
// length, bad encoding, mismatch) — callers don't need to differentiate
// because the response is the same: reject the request.
func VerifyPeer(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// IdentityStore persists this server's Ed25519 identity. Exactly one row
// in server_identity holds it; the constructor LoadOrCreate guarantees
// it exists.
//
// Concurrency: the in-memory cache is RW-mutex-guarded so identity
// reads (hot path: every outbound request) take a read lock, and the
// only writer is initial bootstrap or future Rotate calls.
type IdentityStore struct {
	repo  IdentityRepo
	clock clock.Clock

	mu       sync.RWMutex
	identity *Identity
}

// IdentityRepo is the slice of database operations the IdentityStore
// needs. Declaring it here keeps federation testable with an in-memory
// fake without dragging in db/sql.
type IdentityRepo interface {
	GetIdentity(ctx context.Context) (*Identity, error)
	InsertIdentity(ctx context.Context, id *Identity) error
	// UpdateIdentityProfile persiste cambios al nombre visible y al color
	// hex del avatar. La foto se gestiona por separado en SetAvatarPath.
	UpdateIdentityProfile(ctx context.Context, name, avatarColor string) error
	// SetAvatarPath guarda la ruta relativa al avatar subido (nombre
	// de fichero dentro de avatarsDir). Cadena vacia significa "sin
	// avatar"; el frontend cae a iniciales sobre el color.
	SetAvatarPath(ctx context.Context, path string) error
}

// NewIdentityStore loads the persisted identity into memory.
// LoadOrCreate must have been called once before in this server's
// lifetime (typically by main.go startup); after that, NewIdentityStore
// is just a cache loader.
func NewIdentityStore(ctx context.Context, repo IdentityRepo, clk clock.Clock) (*IdentityStore, error) {
	is := &IdentityStore{repo: repo, clock: clk}
	id, err := repo.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, domain.ErrServerIdentityMissing
	}
	is.identity = id
	return is, nil
}

// LoadOrCreate returns the persisted identity, generating + persisting a
// fresh one if none exists. Idempotent and safe to call at every boot.
func LoadOrCreate(ctx context.Context, repo IdentityRepo, clk clock.Clock, displayName string) (*Identity, error) {
	existing, err := repo.GetIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("federation: load identity: %w", err)
	}
	if existing != nil {
		return existing, nil
	}
	id, err := newIdentity(displayName, clk.Now())
	if err != nil {
		return nil, err
	}
	if err := repo.InsertIdentity(ctx, id); err != nil {
		return nil, fmt.Errorf("federation: persist identity: %w", err)
	}
	return id, nil
}

// Current returns the in-memory identity. The hot path for every
// outbound peer request.
func (is *IdentityStore) Current() *Identity {
	is.mu.RLock()
	defer is.mu.RUnlock()
	return is.identity
}

// UpdateProfile persiste el nombre visible + color hex y refresca la
// cache en memoria. El nombre se valida en el caller (handler) — aqui
// solo dejamos pasar lo que ya está saneado.
func (is *IdentityStore) UpdateProfile(ctx context.Context, name, avatarColor string) error {
	if name == "" {
		return errors.New("federation: identity name required")
	}
	if err := is.repo.UpdateIdentityProfile(ctx, name, avatarColor); err != nil {
		return fmt.Errorf("federation: update identity profile: %w", err)
	}
	is.mu.Lock()
	if is.identity != nil {
		cp := *is.identity
		cp.Name = name
		cp.AvatarColor = avatarColor
		is.identity = &cp
	}
	is.mu.Unlock()
	return nil
}

// SetAvatarPath actualiza la ruta de la foto del servidor (relativa
// al avatarsDir) y refresca la cache. Cadena vacia limpia el avatar.
func (is *IdentityStore) SetAvatarPath(ctx context.Context, path string) error {
	if err := is.repo.SetAvatarPath(ctx, path); err != nil {
		return fmt.Errorf("federation: set avatar path: %w", err)
	}
	is.mu.Lock()
	if is.identity != nil {
		cp := *is.identity
		cp.AvatarImagePath = path
		is.identity = &cp
	}
	is.mu.Unlock()
	return nil
}

// newIdentity generates a fresh Ed25519 keypair + UUID. Pure helper;
// no DB interaction.
func newIdentity(name string, now time.Time) (*Identity, error) {
	if name == "" {
		return nil, errors.New("federation: identity name required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("federation: generate keypair: %w", err)
	}
	return &Identity{
		ServerUUID: uuid.NewString(),
		Name:       name,
		PublicKey:  pub,
		PrivateKey: priv,
		CreatedAt:  now,
	}, nil
}
