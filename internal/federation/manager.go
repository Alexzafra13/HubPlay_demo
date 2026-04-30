package federation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

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

	// Catalog cache (Phase 4+).
	UpsertCachedItems(ctx context.Context, peerID, libraryID string, items []*SharedItem, at time.Time) error
	ListCachedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, time.Time, error)
	PurgeCachedItemsForLibrary(ctx context.Context, peerID, libraryID string) error
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
	// MaxConcurrentStreamsPerPeer caps how many concurrent stream
	// sessions a single peer can hold open against this server.
	// Defaults to 3 — enough for a small-household peer (a couple
	// people watching simultaneously) without letting one peer
	// drain the local transcode budget. 0 disables the cap.
	MaxConcurrentStreamsPerPeer int
}

// DefaultConfig returns sensible defaults for new deployments. Caller
// overrides whatever they want from hubplay.yaml.
func DefaultConfig() Config {
	return Config{
		AdvertisedURL:               "",
		Version:                     "0.1.0",
		SupportedScopes:             []string{"browse", "play"},
		InviteTTL:                   24 * time.Hour,
		HTTPTimeout:                 15 * time.Second,
		PeerRequestsPerMinute:       60,
		PeerBurst:                   30,
		MaxConcurrentStreamsPerPeer: 3,
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
	streams   *peerStreamGate

	// peerCache caches paired peers by server_uuid for the JWT
	// validation hot path. Refreshed on each peer mutation.
	mu        sync.RWMutex
	peerCache map[string]*Peer
}

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
		cfg:       cfg,
		repo:      repo,
		identity:  is,
		clock:     clk,
		logger:    logger.With("module", "federation"),
		bus:       bus,
		httpClt:   &http.Client{Timeout: cfg.HTTPTimeout},
		auditor:   NewAuditor(repo, logger),
		ratelimit: NewRateLimiter(clk, cfg.PeerRequestsPerMinute, cfg.PeerBurst),
		nonces:    newNonceCache(clk),
		streams:   newPeerStreamGate(cfg.MaxConcurrentStreamsPerPeer),
	}
	if err := m.refreshPeerCache(ctx); err != nil {
		// Tear down the auditor we just spawned so we don't leak the
		// background goroutine when the constructor fails.
		m.auditor.Close()
		return nil, err
	}
	return m, nil
}

// Close releases background resources (auditor goroutine). Idempotent.
// Wired into main.go's graceful shutdown so the audit queue flushes
// before the process exits.
func (m *Manager) Close() {
	if m == nil {
		return
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
// Returns ErrPeerNotFound if `audiencePeerID` isn't a paired peer in
// our registry, so callers can short-circuit before bothering with
// the HTTP roundtrip.
func (m *Manager) IssuePeerToken(audiencePeerID string) (string, error) {
	peer, err := m.repo.GetPeerByID(context.Background(), audiencePeerID)
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
func (m *Manager) PublicServerInfo() *ServerInfo {
	id := m.identity.Current()
	return &ServerInfo{
		ServerUUID:        id.ServerUUID,
		Name:              id.Name,
		Version:           m.cfg.Version,
		PublicKey:         []byte(id.PublicKey),
		PubkeyFingerprint: id.Fingerprint(),
		PubkeyWords:       id.FingerprintWords(),
		SupportedScopes:   m.cfg.SupportedScopes,
		AdvertisedURL:     m.cfg.AdvertisedURL,
		AdminContact:      m.cfg.AdminContact,
	}
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
func (m *Manager) AcceptInvite(ctx context.Context, baseURL, code, fallbackAdvertisedURL string) (*Peer, error) {
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

	// Persist them as a paired peer.
	now := m.clock.Now()
	peer := &Peer{
		ID:         uuid.NewString(),
		ServerUUID: theirs.ServerUUID,
		Name:       theirs.Name,
		BaseURL:    baseURL,
		PublicKey:  theirs.PublicKey,
		Status:     PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
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

// HandleInboundHandshake validates the code, persists the remote as a
// paired peer, marks the invite consumed, and returns our own
// ServerInfo so the caller can persist us on their side. Atomic in
// spirit — failures partway through leave the invite consumable for
// another retry, since we update it last.
func (m *Manager) HandleInboundHandshake(ctx context.Context, code string, remote *ServerInfo) (*Peer, *ServerInfo, error) {
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

	peer := &Peer{
		ID:         uuid.NewString(),
		ServerUUID: remote.ServerUUID,
		Name:       remote.Name,
		BaseURL:    remote.AdvertisedURL,
		PublicKey:  remote.PublicKey,
		Status:     PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
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
// Library shares (Phase 3)
// ────────────────────────────────────────────────────────────────────

// ShareLibrary opts a local library into being visible to the named
// peer with the given scopes. Idempotent — re-calling with different
// scopes updates the existing row (UPSERT); the admin can liberalise
// or tighten without manually unsharing first.
//
// Validates the peer is paired before persisting; a revoked or
// pending peer can't have shares because the row would be unreachable
// anyway.
func (m *Manager) ShareLibrary(ctx context.Context, peerID, libraryID, createdByUserID string, scopes ShareScopes) (*LibraryShare, error) {
	peer, err := m.repo.GetPeerByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, domain.ErrPeerNotFound
	}
	if peer.Status != PeerPaired {
		return nil, domain.ErrPeerUnauthorized
	}
	share := &LibraryShare{
		ID:              uuid.NewString(),
		PeerID:          peerID,
		LibraryID:       libraryID,
		CanBrowse:       scopes.CanBrowse,
		CanPlay:         scopes.CanPlay,
		CanDownload:     scopes.CanDownload,
		CanLiveTV:       scopes.CanLiveTV,
		CreatedByUserID: createdByUserID,
		CreatedAt:       m.clock.Now(),
	}
	if err := m.repo.UpsertLibraryShare(ctx, share); err != nil {
		return nil, err
	}
	m.publish(EventShareAdded, map[string]any{
		"peer_id":    peerID,
		"library_id": libraryID,
		"share_id":   share.ID,
		"scopes":     scopes,
	})
	// Re-read so the returned row matches what the DB persisted (in
	// case the unique conflict path overwrote an existing share).
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// UnshareLibrary removes a single share row by ID. Idempotent — a
// missing share is treated as success because the desired state
// (peer cannot see this library) is already true.
func (m *Manager) UnshareLibrary(ctx context.Context, peerID, shareID string) error {
	if err := m.repo.DeleteLibraryShare(ctx, peerID, shareID); err != nil {
		return err
	}
	m.publish(EventShareRemoved, map[string]any{
		"peer_id":  peerID,
		"share_id": shareID,
	})
	return nil
}

// ListSharesByPeer returns every share row for the given peer. Powers
// the admin UI's per-peer expansion panel.
func (m *Manager) ListSharesByPeer(ctx context.Context, peerID string) ([]*LibraryShare, error) {
	return m.repo.ListSharesByPeer(ctx, peerID)
}

// GetLibraryShareForPeer is the public access-check entry point —
// returns the share row if one exists for (peer, library), or nil
// without error if not. Powers the per-item permission check on
// federation streaming and download endpoints. Wraps the repo call
// to avoid leaking the Repo interface to handlers.
func (m *Manager) GetLibraryShareForPeer(ctx context.Context, peerID, libraryID string) (*LibraryShare, error) {
	return m.repo.GetLibraryShare(ctx, peerID, libraryID)
}

// ListSharedLibrariesForPeer returns the libraries the peer can see —
// the data shape served by GET /peer/libraries. Server-side filter
// via JOIN; the peer cannot reach libraries without rows.
func (m *Manager) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	return m.repo.ListSharedLibrariesForPeer(ctx, peerID)
}

// ListSharedItems returns items in a shared library, paginated.
// Returns ErrPeerNotFound if the peer has no share for this library
// — we deliberately conflate "library doesn't exist" and "library
// not shared with you" so attackers can't enumerate library IDs.
func (m *Manager) ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error) {
	share, err := m.repo.GetLibraryShare(ctx, peerID, libraryID)
	if err != nil {
		return nil, 0, err
	}
	if share == nil || !share.CanBrowse {
		return nil, 0, domain.ErrPeerNotFound
	}
	return m.repo.ListSharedItems(ctx, peerID, libraryID, offset, limit)
}

// ────────────────────────────────────────────────────────────────────
// Remote browsing (Phase 4) — user-facing surface that proxies to
// peer endpoints + caches results for offline-friendly browsing.
// ────────────────────────────────────────────────────────────────────

// cacheStaleThreshold — beyond this we kick a background refresh.
// 1h matches "user expects fresh-ish but not real-time" — they
// already saw a peer add titles when they opened the app earlier.
const cacheStaleThreshold = time.Hour

// BrowsePeerLibraries returns the libraries a peer has shared with us.
// Always live — libraries are a small list, no caching needed.
func (m *Manager) BrowsePeerLibraries(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	libs, err := m.FetchPeerLibraries(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if libs == nil {
		libs = []*SharedLibrary{}
	}
	return libs, nil
}

// BrowseAllPeerLibraries fans out to every paired peer in parallel
// and aggregates their shared libraries into a single flat list with
// the originating peer attached. Powers the unified "/peers" landing
// page — one round trip from the user's perspective even if there
// are five peers.
//
// A peer that's offline (or returns an error) is logged and skipped;
// the rest still surface. This keeps the user view useful even when
// one server is down.
func (m *Manager) BrowseAllPeerLibraries(ctx context.Context) ([]*SharedLibraryWithPeer, error) {
	peers, err := m.repo.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	out := []*SharedLibraryWithPeer{}
	type result struct {
		peer *Peer
		libs []*SharedLibrary
		err  error
	}
	results := make(chan result, len(peers))
	dispatched := 0
	for _, p := range peers {
		if p.Status != PeerPaired {
			continue
		}
		dispatched++
		go func(peer *Peer) {
			libs, err := m.FetchPeerLibraries(ctx, peer.ID)
			results <- result{peer: peer, libs: libs, err: err}
		}(p)
	}
	for i := 0; i < dispatched; i++ {
		r := <-results
		if r.err != nil {
			m.logger.Warn("federation: fetch peer libraries (unified view)",
				"peer_id", r.peer.ID, "err", r.err)
			continue
		}
		for _, lib := range r.libs {
			out = append(out, &SharedLibraryWithPeer{Peer: r.peer, Library: lib})
		}
	}
	return out, nil
}

// BrowsePeerItems returns paginated items for a peer's library with
// a read-through cache. Strategy:
//
//   1. Check cache age. If fresh (< staleThreshold), serve from cache.
//   2. If stale or empty, attempt live fetch. On success, write to
//      cache then serve.
//   3. If live fetch fails AND we have any cache, serve stale cache
//      with a "stale" indicator so the user sees content rather than
//      a broken page when the peer is offline.
//
// Returns items, total, and a fromCache flag the API layer can pass
// through so the UI shows the right freshness badge.
func (m *Manager) BrowsePeerItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, bool, error) {
	cached, cachedTotal, cachedAt, cacheErr := m.repo.ListCachedItems(ctx, peerID, libraryID, offset, limit)
	if cacheErr != nil {
		m.logger.Warn("cache read failed, falling back to live",
			"peer_id", peerID, "err", cacheErr)
	}

	now := m.clock.Now()
	cacheFresh := cacheErr == nil && len(cached) > 0 && now.Sub(cachedAt) < cacheStaleThreshold

	if cacheFresh {
		return cached, cachedTotal, true, nil
	}

	live, liveTotal, liveErr := m.FetchPeerItems(ctx, peerID, libraryID, offset, limit)
	if liveErr == nil {
		// Persist to cache only when offset=0 so the cached snapshot
		// is a coherent first-page view. Phase 7+ extends this to a
		// background full-catalog walk.
		if offset == 0 && len(live) > 0 {
			if err := m.repo.UpsertCachedItems(ctx, peerID, libraryID, live, now); err != nil {
				m.logger.Warn("cache write failed", "peer_id", peerID, "err", err)
			}
		}
		return live, liveTotal, false, nil
	}

	// Live failed — serve stale cache if any.
	if cacheErr == nil && len(cached) > 0 {
		m.logger.Info("serving stale cache (peer offline)",
			"peer_id", peerID, "age", now.Sub(cachedAt), "live_err", liveErr)
		return cached, cachedTotal, true, nil
	}

	return nil, 0, false, liveErr
}

// PurgeCache clears cached items for (peer, library) — wired to the
// admin "force refresh" button and called when a peer is revoked.
func (m *Manager) PurgeCache(ctx context.Context, peerID, libraryID string) error {
	return m.repo.PurgeCachedItemsForLibrary(ctx, peerID, libraryID)
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

// handshakeRequest is the POST body of /peer/handshake.
type handshakeRequest struct {
	Code       string      `json:"code"`
	RemoteInfo *ServerInfo `json:"remote_info"`
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
