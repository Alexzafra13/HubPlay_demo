package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// FederationRepository persists server identity, peers, and invites.
//
// This is one of the few places in HubPlay using raw QueryContext
// rather than sqlc-generated code. Reasons:
//
//  1. The schema is brand-new (migration 020) and small enough that
//     the sqlc round-trip would slow down iteration on the design.
//  2. The blob columns (Ed25519 keys) and asymmetric pairing flow
//     don't map cleanly to sqlc's existing patterns yet.
//
// Migration to sqlc-generated code is tracked as Phase 2+ housekeeping;
// the public API of this repo will not change so callers are not
// affected.
type FederationRepository struct {
	db *sql.DB
}

// NewFederationRepository constructs a repo bound to the connection.
func NewFederationRepository(db *sql.DB) *FederationRepository {
	return &FederationRepository{db: db}
}

// ─── server identity ────────────────────────────────────────────────

// GetIdentity returns the singleton row, or (nil, nil) if none yet.
// nil-without-error is the contract the IdentityStore expects so it
// can decide whether to bootstrap a fresh keypair.
func (r *FederationRepository) GetIdentity(ctx context.Context) (*federation.Identity, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT server_uuid, name, private_key, public_key, created_at, rotated_at
		  FROM server_identity
		 WHERE id = 1
	`)
	var (
		uuidStr   string
		name      string
		priv, pub []byte
		createdAt time.Time
		rotatedAt sql.NullTime
	)
	err := row.Scan(&uuidStr, &name, &priv, &pub, &createdAt, &rotatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get server identity: %w", err)
	}
	id := &federation.Identity{
		ServerUUID: uuidStr,
		Name:       name,
		PrivateKey: priv,
		PublicKey:  pub,
		CreatedAt:  createdAt,
	}
	if rotatedAt.Valid {
		t := rotatedAt.Time
		id.RotatedAt = &t
	}
	return id, nil
}

// InsertIdentity persists the singleton. Idempotency guard: errors
// on a second call (table CHECK enforces id=1; UNIQUE on server_uuid
// is the actual collision).
func (r *FederationRepository) InsertIdentity(ctx context.Context, id *federation.Identity) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO server_identity
		    (id, server_uuid, name, private_key, public_key, created_at)
		VALUES (1, ?, ?, ?, ?, ?)
	`, id.ServerUUID, id.Name, []byte(id.PrivateKey), []byte(id.PublicKey), id.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert server identity: %w", err)
	}
	return nil
}

// ─── invites ────────────────────────────────────────────────────────

func (r *FederationRepository) InsertInvite(ctx context.Context, inv *federation.Invite) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO federation_invites
		    (id, code, created_by_user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, inv.ID, inv.Code, inv.CreatedByUserID, inv.CreatedAt, inv.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	return nil
}

func (r *FederationRepository) GetInviteByCode(ctx context.Context, code string) (*federation.Invite, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, created_by_user_id, created_at, expires_at,
		       accepted_by_peer_id, accepted_at
		  FROM federation_invites
		 WHERE code = ?
	`, code)
	var (
		inv          federation.Invite
		acceptedPeer sql.NullString
		acceptedAt   sql.NullTime
	)
	err := row.Scan(&inv.ID, &inv.Code, &inv.CreatedByUserID,
		&inv.CreatedAt, &inv.ExpiresAt, &acceptedPeer, &acceptedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get invite by code: %w", err)
	}
	if acceptedPeer.Valid {
		v := acceptedPeer.String
		inv.AcceptedByPeerID = &v
	}
	if acceptedAt.Valid {
		t := acceptedAt.Time
		inv.AcceptedAt = &t
	}
	return &inv, nil
}

func (r *FederationRepository) MarkInviteUsed(ctx context.Context, inviteID, peerID string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE federation_invites
		   SET accepted_by_peer_id = ?, accepted_at = ?
		 WHERE id = ? AND accepted_at IS NULL
	`, peerID, at, inviteID)
	if err != nil {
		return fmt.Errorf("mark invite used: %w", err)
	}
	return nil
}

func (r *FederationRepository) ListActiveInvites(ctx context.Context) ([]*federation.Invite, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, created_by_user_id, created_at, expires_at,
		       accepted_by_peer_id, accepted_at
		  FROM federation_invites
		 WHERE accepted_at IS NULL AND expires_at > ?
		 ORDER BY created_at DESC
	`, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list active invites: %w", err)
	}
	defer rows.Close()

	out := []*federation.Invite{}
	for rows.Next() {
		var (
			inv          federation.Invite
			acceptedPeer sql.NullString
			acceptedAt   sql.NullTime
		)
		if err := rows.Scan(&inv.ID, &inv.Code, &inv.CreatedByUserID,
			&inv.CreatedAt, &inv.ExpiresAt, &acceptedPeer, &acceptedAt); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		if acceptedPeer.Valid {
			v := acceptedPeer.String
			inv.AcceptedByPeerID = &v
		}
		if acceptedAt.Valid {
			t := acceptedAt.Time
			inv.AcceptedAt = &t
		}
		out = append(out, &inv)
	}
	return out, rows.Err()
}

// ─── peers ──────────────────────────────────────────────────────────

func (r *FederationRepository) InsertPeer(ctx context.Context, p *federation.Peer) error {
	var pairedAt sql.NullTime
	if p.PairedAt != nil {
		pairedAt = sql.NullTime{Time: *p.PairedAt, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO federation_peers
		    (id, server_uuid, name, base_url, public_key, status, created_at, paired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.ServerUUID, p.Name, p.BaseURL, []byte(p.PublicKey),
		string(p.Status), p.CreatedAt, pairedAt)
	if err != nil {
		return fmt.Errorf("insert peer: %w", err)
	}
	return nil
}

func (r *FederationRepository) UpdatePeerPaired(ctx context.Context, peerID string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE federation_peers
		   SET status = 'paired', paired_at = ?
		 WHERE id = ?
	`, at, peerID)
	if err != nil {
		return fmt.Errorf("update peer paired: %w", err)
	}
	return nil
}

func (r *FederationRepository) UpdatePeerRevoked(ctx context.Context, peerID string, at time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE federation_peers
		   SET status = 'revoked', revoked_at = ?
		 WHERE id = ? AND status != 'revoked'
	`, at, peerID)
	if err != nil {
		return fmt.Errorf("update peer revoked: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrPeerNotFound
	}
	return nil
}

func (r *FederationRepository) UpdatePeerLastSeen(ctx context.Context, peerID string, at time.Time, statusCode int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE federation_peers
		   SET last_seen_at = ?, last_seen_status_code = ?
		 WHERE id = ?
	`, at, statusCode, peerID)
	if err != nil {
		return fmt.Errorf("update peer last seen: %w", err)
	}
	return nil
}

func (r *FederationRepository) GetPeerByID(ctx context.Context, id string) (*federation.Peer, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, server_uuid, name, base_url, public_key, status,
		       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
		  FROM federation_peers
		 WHERE id = ?
	`, id)
	return scanPeer(row)
}

func (r *FederationRepository) GetPeerByServerUUID(ctx context.Context, serverUUID string) (*federation.Peer, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, server_uuid, name, base_url, public_key, status,
		       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
		  FROM federation_peers
		 WHERE server_uuid = ?
	`, serverUUID)
	return scanPeer(row)
}

func (r *FederationRepository) ListPeers(ctx context.Context) ([]*federation.Peer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_uuid, name, base_url, public_key, status,
		       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
		  FROM federation_peers
		 ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer rows.Close()

	out := []*federation.Peer{}
	for rows.Next() {
		p, err := scanPeerRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ─── scanning helpers ───────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPeer(row rowScanner) (*federation.Peer, error) {
	var (
		p              federation.Peer
		statusStr      string
		pairedAt       sql.NullTime
		lastSeen       sql.NullTime
		lastSeenStatus sql.NullInt64
		revokedAt      sql.NullTime
		pubKey         []byte
	)
	err := row.Scan(&p.ID, &p.ServerUUID, &p.Name, &p.BaseURL, &pubKey, &statusStr,
		&p.CreatedAt, &pairedAt, &lastSeen, &lastSeenStatus, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPeerNotFound
		}
		return nil, fmt.Errorf("scan peer: %w", err)
	}
	p.PublicKey = pubKey
	p.Status = federation.PeerStatus(statusStr)
	if pairedAt.Valid {
		t := pairedAt.Time
		p.PairedAt = &t
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		p.LastSeenAt = &t
	}
	if lastSeenStatus.Valid {
		v := int(lastSeenStatus.Int64)
		p.LastSeenStatusCode = &v
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		p.RevokedAt = &t
	}
	return &p, nil
}

func scanPeerRow(rows *sql.Rows) (*federation.Peer, error) {
	return scanPeer(rows)
}

// ─── audit log ──────────────────────────────────────────────────────

// InsertAuditEntry persists one peer-request audit row. Idempotency
// isn't a concern — every call is a new event. NULLs flow through
// for optional fields so the table stays honest about what each
// request actually touched.
func (r *FederationRepository) InsertAuditEntry(ctx context.Context, e *federation.AuditEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO federation_audit_log
		    (peer_id, remote_user_id, method, endpoint, status_code,
		     bytes_out, item_id, session_id, error_kind, duration_ms,
		     occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		e.PeerID,
		nullableString(e.RemoteUserID),
		e.Method,
		e.Endpoint,
		e.StatusCode,
		e.BytesOut,
		nullableString(e.ItemID),
		nullableString(e.SessionID),
		nullableString(e.ErrorKind),
		e.DurationMs,
		e.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

// ListAuditEntries returns the most recent N audit rows for a peer,
// newest first. Powers the admin UI's per-peer audit view.
func (r *FederationRepository) ListAuditEntries(ctx context.Context, peerID string, limit int) ([]*federation.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT peer_id, remote_user_id, method, endpoint, status_code,
		       bytes_out, item_id, session_id, error_kind, duration_ms,
		       occurred_at
		  FROM federation_audit_log
		 WHERE peer_id = ?
		 ORDER BY occurred_at DESC
		 LIMIT ?
	`, peerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	out := []*federation.AuditEntry{}
	for rows.Next() {
		var (
			e            federation.AuditEntry
			remoteUser   sql.NullString
			itemID       sql.NullString
			sessionID    sql.NullString
			errorKind    sql.NullString
		)
		if err := rows.Scan(&e.PeerID, &remoteUser, &e.Method, &e.Endpoint,
			&e.StatusCode, &e.BytesOut, &itemID, &sessionID,
			&errorKind, &e.DurationMs, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		if remoteUser.Valid {
			e.RemoteUserID = remoteUser.String
		}
		if itemID.Valid {
			e.ItemID = itemID.String
		}
		if sessionID.Valid {
			e.SessionID = sessionID.String
		}
		if errorKind.Valid {
			e.ErrorKind = errorKind.String
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ─── library shares ────────────────────────────────────────────────

// UpsertLibraryShare inserts or replaces a share row. SQLite UPSERT
// via ON CONFLICT (peer_id, library_id) — the unique constraint
// guarantees one share per (peer, library), and re-sharing simply
// updates scopes without manual delete-then-insert.
func (r *FederationRepository) UpsertLibraryShare(ctx context.Context, s *federation.LibraryShare) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO federation_library_shares
		    (id, peer_id, library_id, can_browse, can_play, can_download,
		     can_livetv, extra_scopes, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(peer_id, library_id) DO UPDATE SET
		    can_browse   = excluded.can_browse,
		    can_play     = excluded.can_play,
		    can_download = excluded.can_download,
		    can_livetv   = excluded.can_livetv,
		    extra_scopes = excluded.extra_scopes
	`,
		s.ID, s.PeerID, s.LibraryID,
		s.CanBrowse, s.CanPlay, s.CanDownload, s.CanLiveTV,
		nullableString(s.ExtraScopes),
		s.CreatedByUserID, s.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert library share: %w", err)
	}
	return nil
}

// DeleteLibraryShare removes a share row. Filters by peer_id too so
// a share-id leak from one peer can't be used to delete another
// peer's share.
func (r *FederationRepository) DeleteLibraryShare(ctx context.Context, peerID, shareID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM federation_library_shares
		 WHERE peer_id = ? AND id = ?
	`, peerID, shareID)
	if err != nil {
		return fmt.Errorf("delete library share: %w", err)
	}
	return nil
}

// GetLibraryShare returns the share row for a given (peer, library)
// pair, or (nil, nil) if no row exists. The nil-without-error contract
// lets callers branch on "share doesn't exist" without error-juggling.
func (r *FederationRepository) GetLibraryShare(ctx context.Context, peerID, libraryID string) (*federation.LibraryShare, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, peer_id, library_id, can_browse, can_play,
		       can_download, can_livetv, extra_scopes, created_by, created_at
		  FROM federation_library_shares
		 WHERE peer_id = ? AND library_id = ?
	`, peerID, libraryID)
	return scanLibraryShare(row)
}

// ListSharesByPeer returns every share row for a peer. Powers the
// admin UI per-peer expansion panel.
func (r *FederationRepository) ListSharesByPeer(ctx context.Context, peerID string) ([]*federation.LibraryShare, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, peer_id, library_id, can_browse, can_play,
		       can_download, can_livetv, extra_scopes, created_by, created_at
		  FROM federation_library_shares
		 WHERE peer_id = ?
		 ORDER BY created_at DESC
	`, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shares by peer: %w", err)
	}
	defer rows.Close()

	out := []*federation.LibraryShare{}
	for rows.Next() {
		s, err := scanLibraryShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSharedLibrariesForPeer is the read for GET /peer/libraries.
// JOIN-filtered: a peer cannot see libraries without a share row.
// Empty rows is the legitimate "you have no shares yet" case.
func (r *FederationRepository) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*federation.SharedLibrary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.content_type,
		       s.can_browse, s.can_play, s.can_download, s.can_livetv
		  FROM federation_library_shares s
		  JOIN libraries l ON l.id = s.library_id
		 WHERE s.peer_id = ?
		 ORDER BY l.name COLLATE NOCASE ASC
	`, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shared libraries: %w", err)
	}
	defer rows.Close()

	out := []*federation.SharedLibrary{}
	for rows.Next() {
		var lib federation.SharedLibrary
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.ContentType,
			&lib.Scopes.CanBrowse, &lib.Scopes.CanPlay,
			&lib.Scopes.CanDownload, &lib.Scopes.CanLiveTV); err != nil {
			return nil, fmt.Errorf("scan shared library: %w", err)
		}
		out = append(out, &lib)
	}
	return out, rows.Err()
}

// ListSharedItems is the read for GET /peer/libraries/{id}/items.
// Caller must have already validated the share + scope; this query
// trusts those assertions for performance and JOIN-filters against
// federation_library_shares as defence in depth.
//
// Returns the slice + total count. Total drives pagination UI.
func (r *FederationRepository) ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*federation.SharedItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Count first — pagination UI needs total. JOIN against shares so
	// this can't return non-zero for a peer without a share.
	var total int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM items i
		  JOIN federation_library_shares s ON s.library_id = i.library_id
		 WHERE i.library_id = ? AND s.peer_id = ? AND s.can_browse = 1
		   AND i.parent_id IS NULL
	`, libraryID, peerID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count shared items: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT i.id, i.type, i.title,
		       COALESCE(i.year, 0),
		       COALESCE(i.overview, '')
		  FROM items i
		  JOIN federation_library_shares s ON s.library_id = i.library_id
		 WHERE i.library_id = ? AND s.peer_id = ? AND s.can_browse = 1
		   AND i.parent_id IS NULL
		 ORDER BY i.title COLLATE NOCASE ASC
		 LIMIT ? OFFSET ?
	`, libraryID, peerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list shared items: %w", err)
	}
	defer rows.Close()

	out := []*federation.SharedItem{}
	for rows.Next() {
		var it federation.SharedItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.Overview); err != nil {
			return nil, 0, fmt.Errorf("scan shared item: %w", err)
		}
		out = append(out, &it)
	}
	return out, total, rows.Err()
}

func scanLibraryShare(row rowScanner) (*federation.LibraryShare, error) {
	var (
		s           federation.LibraryShare
		extraScopes sql.NullString
	)
	err := row.Scan(&s.ID, &s.PeerID, &s.LibraryID, &s.CanBrowse, &s.CanPlay,
		&s.CanDownload, &s.CanLiveTV, &extraScopes, &s.CreatedByUserID, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan library share: %w", err)
	}
	if extraScopes.Valid {
		s.ExtraScopes = extraScopes.String
	}
	return &s, nil
}

// PruneAuditBefore deletes audit rows older than the cutoff.
// Returns the number of rows removed. Called from a background
// pruner (Phase 7+); for now it's a no-op without a caller.
func (r *FederationRepository) PruneAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM federation_audit_log
		 WHERE occurred_at < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// nullableString lives in session_repository.go — shared across the
// db package so audit columns can stay NULL when the request didn't
// touch a particular dimension.
