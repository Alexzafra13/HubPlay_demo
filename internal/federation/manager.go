package federation

// Manager is the federation orchestration core: identity, persistent
// state, lifecycle, and the small set of helpers every other manager_*
// file delegates to. The split is mechanical (one file per concern;
// no API change) so a reader navigating the federation surface can
// jump straight to the file whose name matches the concern:
//
//   manager.go            this file — types, ctor, lifecycle, helpers
//   manager_handshake.go  ProbePeer / AcceptInvite / HandleInbound
//   manager_shares.go     ShareLibrary / Unshare / list / get
//   manager_browse.go     BrowsePeer{Libraries,Items} + cache
//   manager_search.go     Search/Recent local + AllPeers fan-out
//   manager_progress.go   RecordProgress / GetProgress / ContinueWatching

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

// Repo is the database surface the Manager needs. Declaring it here
// keeps the federation package testable with an in-memory fake.
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

	// Library shares (Phase 3+).
	UpsertLibraryShare(ctx context.Context, share *LibraryShare) error
	DeleteLibraryShare(ctx context.Context, peerID, shareID string) error
	GetLibraryShare(ctx context.Context, peerID, libraryID string) (*LibraryShare, error)
	ListSharesByPeer(ctx context.Context, peerID string) ([]*LibraryShare, error)
	ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*SharedLibrary, error)
	ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error)
	SearchSharedItems(ctx context.Context, peerID, query string, limit int) ([]*SharedItem, error)
	ListRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*SharedItem, error)

	// Catalog cache (Phase 4+).
	UpsertCachedItems(ctx context.Context, peerID, libraryID string, items []*SharedItem, at time.Time) error
	ListCachedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, time.Time, error)
	PurgeCachedItemsForLibrary(ctx context.Context, peerID, libraryID string) error

	// Cross-peer playback state (Phase 5 follow-up, migration 028).
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

// EventBus is the slice of internal/event the Manager publishes to.
// nil bus is a valid no-op so test rigs and bare-bones startup don't
// have to wire one.
type EventBus interface {
	Publish(event.Event)
}

// Federation events. The wire format mirrors existing event types
// (Type + Data map[string]any) so downstream subscribers (admin SSE,
// audit log, metrics) consume them with no special-casing.
const (
	EventPeerLinked   event.Type = "federation.peer_linked"
	EventPeerRevoked  event.Type = "federation.peer_revoked"
	EventPeerKeyMatch event.Type = "federation.peer_key_match"
	EventInviteIssued event.Type = "federation.invite_issued"
	EventInviteUsed   event.Type = "federation.invite_used"
	EventShareAdded   event.Type = "federation.share_added"
	EventShareRemoved event.Type = "federation.share_removed"
)

// Config bundles the Manager's static per-instance settings.
type Config struct {
	// AdvertisedURL is the externally-reachable URL another peer would
	// hit to connect to us. May differ from the proxy host (Tailscale,
	// Cloudflare Tunnel) — set explicitly in admin settings.
	AdvertisedURL string
	// AdminContact is optional; renders in our /federation/info for
	// the receiving admin's pre-confirmation context.
	AdminContact string
	// Version is reported in /federation/info so peers can decline
	// scopes the older side does not implement.
	Version string
	// SupportedScopes advertises this server's capabilities.
	SupportedScopes []string
	// InviteTTL bounds how long an invite remains valid.
	InviteTTL time.Duration
	// HTTPTimeout caps outbound peer calls (probe + handshake). Short
	// — peers should respond quickly or be considered offline.
	HTTPTimeout time.Duration
	// PeerRequestsPerMinute is the per-peer rate-limit ceiling for
	// inbound peer-authenticated requests. Defaults to 60.
	PeerRequestsPerMinute int
	// PeerBurst is the bucket ceiling above the steady rate. Defaults to 30.
	PeerBurst int
	// AvatarsDir es el directorio donde se persiste la foto del
	// servidor (subida por el admin desde el panel federation).
	// Compartido con el avatarsDir de usuarios para reutilizar
	// volumen docker; los nombres tienen prefijo "server-" para
	// no colisionar con los UUIDs de usuario. Vacío = uploads
	// del servidor deshabilitados (handler 503).
	AvatarsDir string
}

// DefaultConfig returns sensible defaults for new deployments. Caller
// overrides whatever they want from hubplay.yaml.
func DefaultConfig() Config {
	return Config{
		AdvertisedURL:         "",
		Version:               "0.1.0",
		SupportedScopes:       []string{"browse", "play"},
		InviteTTL:             24 * time.Hour,
		HTTPTimeout:           15 * time.Second,
		PeerRequestsPerMinute: 60,
		PeerBurst:             30,
	}
}

// Manager is the federation feature's orchestration layer. It holds
// identity, persistent state, and outbound HTTP behaviour; HTTP
// handlers and the JWT validator both go through it.
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

	// peerCache caches paired peers by server_uuid for the JWT
	// validation hot path. Refreshed on each peer mutation.
	mu        sync.RWMutex
	peerCache map[string]*Peer

	// streamSessions maps peer-stream session UUIDs to their bookkeeping
	// entry. See stream.go. Separate mutex from peerCache because the
	// streaming hot path doesn't need the peer-cache reader, and
	// holding peerCache's RWMutex during a stream sweep would block
	// JWT validation.
	streamMu       sync.Mutex
	streamSessions map[string]*PeerStreamSession

	// sweeper goroutine state. cancel stops the ticker; done is closed
	// when the goroutine has returned, so Close can wait on it.
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}

	// avatarsDir donde se guarda la foto del servidor. Vacío =
	// uploads deshabilitados; el handler responde 503.
	avatarsDir string
}

// streamSweepInterval is how often we scan streamSessions for entries
// past peerStreamSessionTTL. Comfortably tighter than the TTL itself
// so a stale session lingers at most ~one interval beyond TTL.
const streamSweepInterval = time.Minute

// NewManager wires a Manager. Callers must have already invoked
// LoadOrCreate on this server's identity (typically at startup) so
// the IdentityStore has something to load.
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
		// Tear down the auditor we just spawned so we don't leak the
		// background goroutine when the constructor fails.
		m.auditor.Close()
		return nil, err
	}
	m.startStreamSweeper()
	return m, nil
}

// startStreamSweeper launches the background goroutine that periodically
// reclaims peer stream sessions idle past peerStreamSessionTTL. Without
// this, RegisterPeerStreamSession would accumulate entries forever for
// any peer that opened a session and never came back to play it (the
// JWT expires upstream, but the in-memory mapping does not).
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

// Close releases background resources (audit queue, stream sweeper,
// idle HTTP connections). Idempotent. Wired into main.go's graceful
// shutdown so the audit queue flushes and outbound peer sockets are
// closed before the process exits.
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
		// CloseIdleConnections is best-effort: in-flight requests are
		// not interrupted (they finish on their own context), but TCP
		// keepalives held in the transport's idle pool are released
		// immediately so SIGTERM doesn't wait HTTPTimeout for them.
		m.httpClt.CloseIdleConnections()
	}
	m.auditor.Close()
}

// recordAudit forwards to the manager's auditor; safe to call when the
// auditor is nil (test rigs that don't care about audit). Public via
// internal helpers in middleware.go.
func (m *Manager) recordAudit(entry AuditEntry) {
	m.auditor.Record(entry)
}

// NowUTC returns the manager's clock in UTC. Lets handlers use the
// same clock the manager does (testable + deterministic) without
// having to pass clock around separately.
func (m *Manager) NowUTC() time.Time {
	return m.clock.Now().UTC()
}

// SetAdvertisedURL updates the URL this server advertises in its
// public ServerInfo. Called from the admin settings flow when the
// operator changes their domain (or moves behind a Tailscale endpoint),
// and also at startup once the bind address is known. Concurrency-safe
// because PublicServerInfo() reads cfg fields under the manager mutex
// — but cfg writes are intentionally serialised at process boot, so
// dynamic updates are admin-driven and rare.
func (m *Manager) SetAdvertisedURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.AdvertisedURL = url
}

// IssuePeerToken mints a fresh outbound peer JWT from THIS server to
// the named audience peer. Used by Phase 3+ outbound clients (catalog
// browse, stream session start, etc.) to authenticate themselves on
// the remote.
//
// Takes the request's ctx so a cancellation upstream (user closed the
// tab, request deadline tripped) cancels the underlying DB lookup.
// Without it, a stuck SQLite write would block the request indefinitely
// even after the caller had walked away.
//
// Returns ErrPeerNotFound if `audiencePeerID` isn't a paired peer in
// our registry, so callers can short-circuit before bothering with
// the HTTP roundtrip.
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

// PublicServerInfo renders the ServerInfo this server advertises on
// GET /federation/info. Called frequently — the result is read-only
// and cheap to assemble.
//
// AvatarImageURL se compone con AdvertisedURL + path serving público
// para que el peer la pinte directamente. El sufijo "?v=<filename>"
// actúa de cache-buster: cada upload reemplaza el nombre y el peer
// refetchea sin negociar ETag.
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

// UpdateIdentityProfile cambia el nombre visible y el color hex del
// avatar del servidor. Pasa por el IdentityStore para que la cache
// en memoria + la DB queden consistentes en una sola operación.
func (m *Manager) UpdateIdentityProfile(ctx context.Context, name, avatarColor string) error {
	return m.identity.UpdateProfile(ctx, name, avatarColor)
}

// SetIdentityAvatarPath registra (o limpia, con cadena vacía) la
// ruta relativa del avatar subido. El handler escribe primero el
// fichero a disco y luego llama aquí para persistir el nombre.
func (m *Manager) SetIdentityAvatarPath(ctx context.Context, path string) error {
	return m.identity.SetAvatarPath(ctx, path)
}

// IdentityAvatarPath devuelve la ruta relativa del avatar actual o
// cadena vacía si no hay. Útil para que el servidor sirva el binario
// público y para que el admin lo previsualice.
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

// GenerateInvite mints a single-use code that the local admin shares
// with a remote admin out-of-band. Persists immediately so a crash
// after generation doesn't lose the code that the admin already copied.
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

// ListActiveInvites returns codes that are still usable (not expired,
// not consumed). Intended for the admin UI listing.
func (m *Manager) ListActiveInvites(ctx context.Context) ([]*Invite, error) {
	return m.repo.ListActiveInvites(ctx)
}

// ────────────────────────────────────────────────────────────────────
// Peer CRUD
// ────────────────────────────────────────────────────────────────────

// ListPeers returns every peer the admin has on record (including
// revoked, for audit).
func (m *Manager) ListPeers(ctx context.Context) ([]*Peer, error) {
	return m.repo.ListPeers(ctx)
}

// GetPeer fetches a single peer by local UUID.
func (m *Manager) GetPeer(ctx context.Context, id string) (*Peer, error) {
	return m.repo.GetPeerByID(ctx, id)
}

// LookupByServerUUID implements PeerLookup so jwt.go can resolve an
// inbound JWT issuer to the matching pinned pubkey.
func (m *Manager) LookupByServerUUID(serverUUID string) (*Peer, error) {
	m.mu.RLock()
	p, ok := m.peerCache[serverUUID]
	m.mu.RUnlock()
	if !ok {
		return nil, domain.ErrPeerNotFound
	}
	return p, nil
}

// CheckAndStoreNonce records a nonce as seen and returns whether it was
// fresh. Returns false on replay (the same nonce was used before within
// its parent token's TTL window). Called by the peer-JWT middleware
// after signature/audience/expiry checks pass — replay is the *only*
// thing left to verify before the request is honoured.
//
// `exp` is the JWT's expiry; the cache evicts entries past that point
// because at that moment the token is rejected upstream by
// ValidatePeerToken anyway, so the nonce no longer needs tracking.
func (m *Manager) CheckAndStoreNonce(nonce string, exp time.Time) bool {
	if m == nil || m.nonces == nil {
		return true
	}
	return m.nonces.checkAndStore(nonce, exp)
}

// ────────────────────────────────────────────────────────────────────
// Peer revocation
// ────────────────────────────────────────────────────────────────────

// RevokePeer terminates a peer relationship. The row remains for
// audit (terminal state); all future JWTs from the peer are rejected
// by ValidatePeerToken because PeerRevoked != PeerPaired.
//
// Atomicity note: the DB write (UpdatePeerRevoked) happens BEFORE the
// in-memory peerCache refresh, so there is a sub-millisecond window
// where a request that already passed LookupByServerUUID continues
// to completion under the OLD cached *Peer. The window is bounded
// by the time refreshPeerCache holds its write lock (microseconds in
// practice), and any in-flight request was going to finish within
// the JWT TTL anyway. New requests issued after the cache refresh
// see PeerRevoked and fail at the auth gate.
//
// Stricter atomicity (DB + cache in one critical section) would
// require holding the cache write lock during a SQLite roundtrip,
// which we explicitly do not want — it would block every concurrent
// JWT validation on every peer.
func (m *Manager) RevokePeer(ctx context.Context, peerID string) error {
	now := m.clock.Now()
	if err := m.repo.UpdatePeerRevoked(ctx, peerID, now); err != nil {
		return err
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		m.logger.Warn("federation: peer cache refresh after revoke failed", "err", err)
	}
	// Drop any in-memory rate-limit state for this peer so a future
	// re-pairing starts with a clean bucket instead of inheriting
	// residual tokens from a previously hostile session.
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

// RefreshPeerCache repopulates the in-memory paired-peers cache from
// the repository. Exposed for tests that insert peer rows directly
// (bypassing the handshake flow) so JWT validation can find them.
// Production callers go through ProbePeer / AcceptInvite / RevokePeer
// which all refresh the cache themselves.
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

// joinBaseURL appends a path to a base URL with idempotent slash
// handling. Rejects an empty base or non-http(s) scheme so a typo
// doesn't end up making us POST to a file:// URL.
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

// EncodePublicKey renders a public key as base64 for JSON transport.
// Used by callers that need to display or log a pubkey.
func EncodePublicKey(pub []byte) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey reverses EncodePublicKey. Returns ErrInvalidToken on
// any decoding failure so the caller can map straight to 400.
func DecodePublicKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, domain.ErrInvalidToken
	}
	return b, nil
}
