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
	// SHA-256(pubkey)[:8] -> 16 hex chars en 4 grupos de 4.
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

// FingerprintWords devuelve 6 palabras pronunciables derivadas del
// fingerprint. 48 bits de confirmacion oral — suficiente para verificacion
// por voz entre admins.
func (i *Identity) FingerprintWords() []string {
	return FingerprintWords(i.PublicKey)
}

// FingerprintWords (variante free-function).
func FingerprintWords(pub ed25519.PublicKey) []string {
	if len(pub) == 0 {
		return nil
	}
	sum := sha256First8(pub)
	out := make([]string, 6)
	// 6 primeros bytes del SHA-256 truncado a 8; cada uno mapea a una palabra.
	for idx := 0; idx < 6; idx++ {
		out[idx] = phoneticWords[int(sum[idx])%len(phoneticWords)]
	}
	return out
}

// Sign firma msg con la privkey de este servidor.
func (i *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(i.PrivateKey, msg)
}

// VerifyPeer verifica firma Ed25519 contra el pubkey pineado del peer.
// False cubre todo modo de fallo; callers solo necesitan rechazar.
func VerifyPeer(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// IdentityStore persiste la identidad Ed25519 del servidor. Cache en
// memoria con RWMutex: lecturas (hot path outbound) con read lock,
// escrituras solo en bootstrap o Rotate.
type IdentityStore struct {
	repo  IdentityRepo
	clock clock.Clock

	mu       sync.RWMutex
	identity *Identity
}

// IdentityRepo es la interfaz de DB del IdentityStore. Declarada aqui
// para mantener federation testable con fake in-memory.
type IdentityRepo interface {
	GetIdentity(ctx context.Context) (*Identity, error)
	InsertIdentity(ctx context.Context, id *Identity) error
	// UpdateIdentityProfile persiste nombre visible + color hex del avatar.
	UpdateIdentityProfile(ctx context.Context, name, avatarColor string) error
	// SetAvatarPath guarda la ruta relativa del avatar. Vacio = sin avatar.
	SetAvatarPath(ctx context.Context, path string) error
}

// NewIdentityStore carga la identidad persistida en memoria.
// LoadOrCreate debe haberse llamado antes (tipicamente en main.go).
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

// LoadOrCreate devuelve la identidad persistida o genera una nueva.
// Idempotente, seguro en cada boot.
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

// Current devuelve la identidad en memoria (hot path outbound).
func (is *IdentityStore) Current() *Identity {
	is.mu.RLock()
	defer is.mu.RUnlock()
	return is.identity
}

// UpdateProfile persiste nombre + color hex y refresca cache en memoria.
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

// SetAvatarPath actualiza la ruta de la foto y refresca cache. Vacio limpia.
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

// newIdentity genera keypair Ed25519 + UUID. Sin interaccion con DB.
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
