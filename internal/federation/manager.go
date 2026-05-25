package federation

// Manager es el nucleo de orquestacion de federation: identidad, estado,
// ciclo de vida y helpers. Un fichero por concern (manager_*.go).

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// Repo es la superficie de DB que el Manager necesita. Interfaz local
// para mantener federation testable con fake in-memory.
type Repo interface {
	IdentityRepo
	AuditRepo

	InsertInvite(ctx context.Context, invite *Invite) error
	GetInviteByCode(ctx context.Context, code string) (*Invite, error)
	MarkInviteUsed(ctx context.Context, inviteID, peerID string, at time.Time) error
	ListActiveInvites(ctx context.Context) ([]*Invite, error)

	InsertPeer(ctx context.Context, peer *Peer) error
	UpdatePeerPaired(ctx context.Context, peerID string, at time.Time) error
	UpdatePeerRevoked(ctx context.Context, peerID string, at time.Time) error
	UpdatePeerLastSeen(ctx context.Context, peerID string, at time.Time, statusCode int) error
	UpdatePeerBranding(ctx context.Context, peerID, name, avatarColor, avatarImageURL string) error
	GetPeerByID(ctx context.Context, id string) (*Peer, error)
	GetPeerByServerUUID(ctx context.Context, serverUUID string) (*Peer, error)
	ListPeers(ctx context.Context) ([]*Peer, error)

	// Library shares.
	UpsertLibraryShare(ctx context.Context, share *LibraryShare) error
	DeleteLibraryShare(ctx context.Context, peerID, shareID string) error
	GetLibraryShare(ctx context.Context, peerID, libraryID string) (*LibraryShare, error)
	ListSharesByPeer(ctx context.Context, peerID string) ([]*LibraryShare, error)
	ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*SharedLibrary, error)
	ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error)
	SearchSharedItems(ctx context.Context, peerID, query string, limit int) ([]*SharedItem, error)
	ListRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*SharedItem, error)

	// Cache de catalogo.
	UpsertCachedItems(ctx context.Context, peerID, libraryID string, items []*SharedItem, at time.Time) error
	ListCachedItems(ctx context.Context, peerID, libraryID string, offset, limit int) (CachedItemPage, error)
	PurgeCachedItemsForLibrary(ctx context.Context, peerID, libraryID string) error

	// Estado de reproduccion cross-peer (migration 028).
	UpsertProgress(ctx context.Context, p *Progress) error
	GetProgress(ctx context.Context, userID, peerID, remoteItemID string) (*Progress, error)
	DeleteProgress(ctx context.Context, userID, peerID, remoteItemID string) error
	ListContinueWatching(ctx context.Context, userID string, limit int) ([]*PeerContinueWatchingItem, error)

	// Pairing requests (migration 048) - flow Steam-style sin codigo.
	InsertPendingRequest(ctx context.Context, p *PendingRequest) error
	GetPendingRequestByID(ctx context.Context, id string) (*PendingRequest, error)
	GetActivePendingRequestByPeer(ctx context.Context, direction PendingRequestDirection, serverUUID string) (*PendingRequest, bool, error)
	ListPendingRequests(ctx context.Context, limit int) ([]*PendingRequest, error)
	MarkPendingRequestResponded(ctx context.Context, id string, status PendingRequestStatus, by string, at time.Time) error
	ExpirePendingRequests(ctx context.Context, before time.Time) (int, error)
	CountUnreadIncomingPendingRequests(ctx context.Context) (int, error)
}

// SettingsReader: interfaz estrecha para leer toggles persistentes.
type SettingsReader interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// EventBus: slice de event que el Manager publica. nil = no-op valido.
type EventBus interface {
	Publish(event.Event)
}

// Eventos de federation. Formato wire compatible con event.Event estandar.
const (
	EventPeerLinked   event.Type = "federation.peer_linked"
	EventPeerRevoked  event.Type = "federation.peer_revoked"
	EventPeerKeyMatch event.Type = "federation.peer_key_match"
	EventInviteIssued event.Type = "federation.invite_issued"
	EventInviteUsed   event.Type = "federation.invite_used"
	EventShareAdded   event.Type = "federation.share_added"
	EventShareRemoved event.Type = "federation.share_removed"
)

// Config agrupa settings estaticos por instancia del Manager.
type Config struct {
	// AdvertisedURL: URL externa a la que peers conectan.
	AdvertisedURL string
	// AdminContact: opcional, se muestra en /federation/info.
	AdminContact string
	// Version: reportada en /federation/info.
	Version string
	// SupportedScopes: capacidades anunciadas.
	SupportedScopes []string
	// InviteTTL: ventana de validez de invitaciones.
	InviteTTL time.Duration
	// HTTPTimeout: techo para llamadas outbound a peers.
	HTTPTimeout time.Duration
	// PeerRequestsPerMinute: rate-limit por peer para requests inbound. Default 60.
	PeerRequestsPerMinute int
	// PeerBurst: techo del bucket sobre la tasa sostenida. Default 30.
	PeerBurst int
	// AvatarsDir: directorio de la foto del servidor. Prefijo "server-"
	// para no colisionar con UUIDs de usuario. Vacio = uploads deshabilitados.
	AvatarsDir string
	// MaxIncomingPendingRequests: cap defensivo de pending incoming.
	// Default 100. 0 = sin cap (no recomendado en prod abierto).
	MaxIncomingPendingRequests int
}

// DefaultConfig devuelve defaults razonables. El caller sobreescribe
// desde hubplay.yaml.
func DefaultConfig() Config {
	return Config{
		AdvertisedURL:         "",
		Version:               "0.1.0",
		SupportedScopes:       []string{"browse", "play"},
		InviteTTL:                  24 * time.Hour,
		HTTPTimeout:                15 * time.Second,
		PeerRequestsPerMinute:      60,
		PeerBurst:                  30,
		MaxIncomingPendingRequests: 100,
	}
}

// Manager es la capa de orquestacion de federation. Mantiene identidad,
// estado persistente y comportamiento HTTP outbound.
type Manager struct {
	cfg       Config
	repo      Repo
	identity  *IdentityStore
	clock     clock.Clock
	logger    *slog.Logger
	bus       EventBus
	httpClt   *http.Client
	auditor   *Auditor
	ratelimit *RateLimiter
	nonces    *nonceCache
	metrics   MetricsSink

	// peerCache: cache de peers paired por server_uuid para hot path JWT.
	mu        sync.RWMutex
	peerCache map[string]*Peer

	// streamSessions: sesiones de streaming activas. Mutex separado de
	// peerCache para no bloquear validacion JWT durante sweep.
	streamMu       sync.Mutex
	streamSessions map[string]*PeerStreamSession

	// Estado del sweeper goroutine.
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}

	// avatarsDir: foto del servidor. Vacio = uploads deshabilitados.
	avatarsDir string

	// settings: inyectado en composition root. Nil-safe.
	settings SettingsReader
}

// streamSweepInterval: cada cuanto se barren sesiones expiradas.
const streamSweepInterval = time.Minute

// NewManager construye un Manager. LoadOrCreate debe haberse llamado antes.
func NewManager(ctx context.Context, cfg Config, repo Repo, clk clock.Clock, logger *slog.Logger, bus EventBus) (*Manager, error) {
	is, err := NewIdentityStore(ctx, repo, clk)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		cfg:            cfg,
		repo:           repo,
		identity:       is,
		clock:          clk,
		logger:         logger.With("module", "federation"),
		bus:            bus,
		httpClt:        &http.Client{Timeout: cfg.HTTPTimeout},
		auditor:        NewAuditor(repo, logger),
		ratelimit:      NewRateLimiter(clk, cfg.PeerRequestsPerMinute, cfg.PeerBurst),
		nonces:         newNonceCache(clk),
		metrics:        noopMetricsSink{},
		streamSessions: make(map[string]*PeerStreamSession),
		avatarsDir:     cfg.AvatarsDir,
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		// Limpiar auditor para no filtrar goroutine si el constructor falla.
		m.auditor.Close()
		return nil, err
	}
	m.startStreamSweeper()
	return m, nil
}

// startStreamSweeper lanza goroutine que reclama sesiones idle pasado TTL.
func (m *Manager) startStreamSweeper() {
	ctx, cancel := context.WithCancel(context.Background())
	m.sweepCancel = cancel
	m.sweepDone = make(chan struct{})
	go func() {
		defer close(m.sweepDone)
		t := time.NewTicker(streamSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.SweepStreamSessions()
			}
		}
	}()
}

// Close libera recursos de fondo (audit, sweeper, conexiones idle).
// Idempotente. Conectado al graceful shutdown de main.go.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	if m.sweepCancel != nil {
		m.sweepCancel()
		<-m.sweepDone
		m.sweepCancel = nil
	}
	if m.httpClt != nil {
		// Best-effort: libera keepalives idle sin interrumpir in-flight.
		m.httpClt.CloseIdleConnections()
	}
	m.auditor.Close()
}

// recordAudit reenvia al auditor. Nil-safe para tests.
func (m *Manager) recordAudit(entry AuditEntry) {
	m.auditor.Record(entry)
}

// NowUTC devuelve el reloj del manager en UTC.
func (m *Manager) NowUTC() time.Time {
	return m.clock.Now().UTC()
}

// SetSettings inyecta el reader de settings persistentes.
func (m *Manager) SetSettings(s SettingsReader) {
	m.settings = s
}

// SettingAcceptPairingRequests: key del toggle "aceptar peticiones
// entrantes". Default ausente = true.
const SettingAcceptPairingRequests = "federation.accept_pairing_requests"

// AcceptingPairingRequests reporta si se admiten nuevas peticiones. Default true.
func (m *Manager) AcceptingPairingRequests(ctx context.Context) bool {
	if m.settings == nil {
		return true
	}
	v, err := m.settings.Get(ctx, SettingAcceptPairingRequests)
	if err != nil {
		// Key sin setear = default "true".
		return true
	}
	return v != "false"
}

// SetAcceptingPairingRequests persiste el toggle admin.
func (m *Manager) SetAcceptingPairingRequests(ctx context.Context, enabled bool) error {
	if m.settings == nil {
		return fmt.Errorf("federation: settings not configured")
	}
	value := "true"
	if !enabled {
		value = "false"
	}
	return m.settings.Set(ctx, SettingAcceptPairingRequests, value)
}

// SetAdvertisedURL actualiza la URL anunciada. Llamada desde admin
// settings o al arrancar cuando se conoce el bind address.
func (m *Manager) SetAdvertisedURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.AdvertisedURL = url
}

// IssuePeerToken genera JWT outbound para el peer audience.
// Devuelve ErrPeerNotFound si no esta paired.
func (m *Manager) IssuePeerToken(ctx context.Context, audiencePeerID string) (string, error) {
	peer, err := m.repo.GetPeerByID(ctx, audiencePeerID)
	if err != nil {
		return "", err
	}
	if peer == nil || peer.Status != PeerPaired {
		return "", domain.ErrPeerNotFound
	}
	id := m.identity.Current()
	return IssuePeerToken(m.clock, id.PrivateKey, id.ServerUUID, peer.ServerUUID)
}

// PublicServerInfo renderiza el ServerInfo que publicamos en /federation/info.
// AvatarImageURL incluye cache-buster via sufijo ?v=<filename>.
func (m *Manager) PublicServerInfo() *ServerInfo {
	id := m.identity.Current()
	info := &ServerInfo{
		ServerUUID:        id.ServerUUID,
		Name:              id.Name,
		Version:           m.cfg.Version,
		PublicKey:         []byte(id.PublicKey),
		PubkeyFingerprint: id.Fingerprint(),
		PubkeyWords:       id.FingerprintWords(),
		SupportedScopes:   m.cfg.SupportedScopes,
		AdvertisedURL:     m.cfg.AdvertisedURL,
		AdminContact:      m.cfg.AdminContact,
		AvatarColor:       id.AvatarColor,
	}
	if id.AvatarImagePath != "" && m.cfg.AdvertisedURL != "" {
		info.AvatarImageURL = strings.TrimRight(m.cfg.AdvertisedURL, "/") +
			"/api/v1/federation/identity/avatar?v=" + id.AvatarImagePath
	}
	return info
}

// UpdateIdentityProfile cambia nombre + color hex via IdentityStore.
func (m *Manager) UpdateIdentityProfile(ctx context.Context, name, avatarColor string) error {
	return m.identity.UpdateProfile(ctx, name, avatarColor)
}

// SetIdentityAvatarPath registra o limpia la ruta del avatar subido.
func (m *Manager) SetIdentityAvatarPath(ctx context.Context, path string) error {
	return m.identity.SetAvatarPath(ctx, path)
}

// IdentityAvatarPath devuelve la ruta del avatar actual o vacio.
func (m *Manager) IdentityAvatarPath() string {
	id := m.identity.Current()
	if id == nil {
		return ""
	}
	return id.AvatarImagePath
}

// ────────────────────────────────────────────────────────────────────
// Invite lifecycle
// ────────────────────────────────────────────────────────────────────

// GenerateInvite genera un codigo de un solo uso. Persiste inmediatamente.
func (m *Manager) GenerateInvite(ctx context.Context, userID string) (*Invite, error) {
	inv, err := NewInvite(m.clock, userID, m.cfg.InviteTTL)
	if err != nil {
		return nil, fmt.Errorf("federation: new invite: %w", err)
	}
	if err := m.repo.InsertInvite(ctx, inv); err != nil {
		return nil, fmt.Errorf("federation: persist invite: %w", err)
	}
	m.publish(EventInviteIssued, map[string]any{
		"invite_id":  inv.ID,
		"created_by": userID,
		"expires_at": inv.ExpiresAt,
	})
	return inv, nil
}

// ListActiveInvites devuelve invitaciones aun usables.
func (m *Manager) ListActiveInvites(ctx context.Context) ([]*Invite, error) {
	return m.repo.ListActiveInvites(ctx)
}

// ────────────────────────────────────────────────────────────────────
// Peer CRUD
// ────────────────────────────────────────────────────────────────────

// ListPeers devuelve todos los peers (incluidos revocados, para audit).
func (m *Manager) ListPeers(ctx context.Context) ([]*Peer, error) {
	return m.repo.ListPeers(ctx)
}

// GetPeer obtiene un peer por UUID local.
func (m *Manager) GetPeer(ctx context.Context, id string) (*Peer, error) {
	return m.repo.GetPeerByID(ctx, id)
}

// LookupByServerUUID implementa PeerLookup para resolver JWT inbound.
func (m *Manager) LookupByServerUUID(serverUUID string) (*Peer, error) {
	m.mu.RLock()
	p, ok := m.peerCache[serverUUID]
	m.mu.RUnlock()
	if !ok {
		return nil, domain.ErrPeerNotFound
	}
	return p, nil
}

// CheckAndStoreNonce registra un nonce y devuelve si era fresco.
// False = replay (nonce ya visto dentro del TTL del token).
func (m *Manager) CheckAndStoreNonce(nonce string, exp time.Time) bool {
	if m == nil || m.nonces == nil {
		return true
	}
	return m.nonces.checkAndStore(nonce, exp)
}

// ────────────────────────────────────────────────────────────────────
// Peer revocation
// ────────────────────────────────────────────────────────────────────

// RevokePeer termina la relacion con un peer. La fila queda para audit.
// La DB se actualiza ANTES del cache refresh; hay una ventana sub-ms
// donde requests in-flight ven el Peer viejo (acotada por el write lock).
func (m *Manager) RevokePeer(ctx context.Context, peerID string) error {
	now := m.clock.Now()
	if err := m.repo.UpdatePeerRevoked(ctx, peerID, now); err != nil {
		return err
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		m.logger.Warn("federation: peer cache refresh after revoke failed", "err", err)
	}
	// Limpiar state de rate-limit para que un re-pair arranque limpio.
	if m.ratelimit != nil {
		m.ratelimit.Reset(peerID)
	}
	m.publish(EventPeerRevoked, map[string]any{
		"peer_id": peerID,
	})
	return nil
}

// ────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────

// RefreshPeerCache repobla la cache de peers desde el repo.
// Expuesto para tests que insertan filas directamente.
func (m *Manager) RefreshPeerCache(ctx context.Context) error {
	return m.refreshPeerCache(ctx)
}

func (m *Manager) refreshPeerCache(ctx context.Context) error {
	peers, err := m.repo.ListPeers(ctx)
	if err != nil {
		return err
	}
	cache := make(map[string]*Peer, len(peers))
	for _, p := range peers {
		if p.Status == PeerPaired {
			cache[p.ServerUUID] = p
		}
	}
	m.mu.Lock()
	m.peerCache = cache
	m.mu.Unlock()
	return nil
}

func (m *Manager) publish(t event.Type, data map[string]any) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(event.Event{Type: t, Data: data})
}

// joinBaseURL concatena path a base URL. Rechaza base vacia o scheme no http(s).
func joinBaseURL(base, path string) (string, error) {
	if base == "" {
		return "", errors.New("base URL is empty")
	}
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return "", fmt.Errorf("base URL must be http(s): %q", base)
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path, nil
}

// EncodePublicKey codifica un pubkey en base64 para transporte JSON.
func EncodePublicKey(pub []byte) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey decodifica base64. Devuelve ErrInvalidToken si falla.
func DecodePublicKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, domain.ErrInvalidToken
	}
	return b, nil
}
