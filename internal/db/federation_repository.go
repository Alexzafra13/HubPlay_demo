package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// FederationRepository is the persistence adapter for the federation
// subsystem. Pattern A dual-dialect: every sqlc-backed method has a
// SQLite branch and a Postgres branch; raw-SQL holdouts (FTS search,
// poster colour batch, federation_item_cache writer/reader) carry
// dialect-specific template variants pre-rewritten at construction
// time.
//
// The raw holdouts exist because:
//
//   - SearchSharedItems uses an FTS clause: SQLite's `items_fts MATCH ?`
//     (FTS5 virtual table) vs Postgres' `search_vector @@ to_tsquery(...)`
//     (tsvector column). sqlc doesn't parse either, hence raw.
//   - attachPrimaryImageColors has a runtime-sized IN list.
//   - ListCachedItems uses `ORDER BY ... COLLATE NOCASE` which is
//     SQLite-only; the Postgres branch substitutes `LOWER(...)`.
//   - UpsertCachedItems' inner INSERT was raw historically because
//     sqlc 1.31.1 truncated the surrounding query file; we keep it
//     raw with dialect-aware placeholder rewrite.
type FederationRepository struct {
	db     *sql.DB
	sq     *sqlc.Queries
	pq     *sqlc_pg.Queries
	driver string

	searchSharedItemsSQL string
	insertCachedItemSQL  string
	listCachedItemsSQL   string
}

// NewFederationRepository binds a repo to the given DB connection.
func NewFederationRepository(driver string, database *sql.DB) *FederationRepository {
	r := &FederationRepository{db: database, driver: driver}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}

	// Pre-rewrite the raw-SQL holdouts. The FTS predicate and the
	// case-insensitive sort use different syntax per dialect; everything
	// else (`?`, BOOLEAN truthy predicates) is portable once rewritten.
	r.searchSharedItemsSQL = rewritePlaceholders(driver, buildSearchSharedItemsSQL(driver))
	r.insertCachedItemSQL = rewritePlaceholders(driver, `
		INSERT INTO federation_item_cache
		    (peer_id, library_id, remote_id, type, title,
		     year, overview, has_poster,
		     poster_color, poster_color_muted, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	r.listCachedItemsSQL = rewritePlaceholders(driver, fmt.Sprintf(`
		SELECT remote_id, type, title,
		       COALESCE(year, 0) AS year,
		       COALESCE(overview, '') AS overview,
		       has_poster, poster_color, poster_color_muted
		FROM federation_item_cache
		WHERE peer_id = ? AND library_id = ?
		ORDER BY %s ASC
		LIMIT ? OFFSET ?`, caseInsensitiveSort(driver, "title")))

	return r
}

func (r *FederationRepository) useSQLite() bool { return r.sq != nil }

// caseInsensitiveSort returns the dialect-specific expression for
// case-insensitive ordering on a column. SQLite has the `COLLATE
// NOCASE` clause; Postgres doesn't recognise it and needs `LOWER(col)`
// (or the citext extension, which we don't depend on). Centralised
// here so a future call site (a third rail) reuses the same recipe.
func caseInsensitiveSort(driver, col string) string {
	if IsPostgres(driver) {
		return "LOWER(" + col + ")"
	}
	return col + " COLLATE NOCASE"
}

// buildSearchSharedItemsSQL stitches the FTS search query per dialect.
// SQLite uses `rowid IN (items_fts MATCH ?)`; Postgres uses the
// `search_vector @@ to_tsquery('simple', ?)` predicate populated by
// the trigger from migrations/postgres/002_fts_search.sql. Caller
// passes the same args slice; only the SQL text varies.
func buildSearchSharedItemsSQL(driver string) string {
	var ftsClause string
	if IsPostgres(driver) {
		ftsClause = "i.search_vector @@ to_tsquery('simple', ?)"
	} else {
		ftsClause = "i.rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)"
	}
	return fmt.Sprintf(`
		SELECT i.id, i.type, i.title,
		       COALESCE(i.year, 0),
		       COALESCE(m.overview, ''),
		       EXISTS (
		         SELECT 1 FROM images img
		          WHERE img.item_id = i.id
		            AND img.type = 'primary'
		            AND img.is_primary
		       ) AS has_poster,
		       i.library_id
		  FROM items i
		  JOIN federation_library_shares s ON s.library_id = i.library_id
		  LEFT JOIN metadata m ON m.item_id = i.id
		 WHERE s.peer_id = ?
		   AND s.can_browse
		   AND i.parent_id IS NULL
		   AND %s
		 ORDER BY %s ASC
		 LIMIT ?`, ftsClause, caseInsensitiveSort(driver, "i.sort_title"))
}

// ─── server identity ────────────────────────────────────────────────

// GetIdentity returns the singleton row, or (nil, nil) if none yet.
// nil-without-error is the contract the IdentityStore expects so it
// can decide whether to bootstrap a fresh keypair.
func (r *FederationRepository) GetIdentity(ctx context.Context) (*federation.Identity, error) {
	if r.useSQLite() {
		row, err := r.sq.GetServerIdentity(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get server identity: %w", err)
		}
		return identityFromSqliteRow(row), nil
	}
	row, err := r.pq.GetServerIdentity(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get server identity: %w", err)
	}
	return identityFromPgRow(row), nil
}

func identityFromSqliteRow(row sqlc.GetServerIdentityRow) *federation.Identity {
	id := &federation.Identity{
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		PrivateKey: row.PrivateKey,
		PublicKey:  row.PublicKey,
		CreatedAt:  row.CreatedAt,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

func identityFromPgRow(row sqlc_pg.GetServerIdentityRow) *federation.Identity {
	id := &federation.Identity{
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		PrivateKey: row.PrivateKey,
		PublicKey:  row.PublicKey,
		CreatedAt:  row.CreatedAt,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

// InsertIdentity persists the singleton. Idempotency guard: errors
// on a second call (CHECK(id=1) + UNIQUE on server_uuid).
func (r *FederationRepository) InsertIdentity(ctx context.Context, id *federation.Identity) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertServerIdentity(ctx, sqlc.InsertServerIdentityParams{
			ServerUuid: id.ServerUUID,
			Name:       id.Name,
			PrivateKey: []byte(id.PrivateKey),
			PublicKey:  []byte(id.PublicKey),
			CreatedAt:  id.CreatedAt,
		})
	} else {
		err = r.pq.InsertServerIdentity(ctx, sqlc_pg.InsertServerIdentityParams{
			ServerUuid: id.ServerUUID,
			Name:       id.Name,
			PrivateKey: []byte(id.PrivateKey),
			PublicKey:  []byte(id.PublicKey),
			CreatedAt:  id.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert server identity: %w", err)
	}
	return nil
}

// ─── invites ────────────────────────────────────────────────────────

func (r *FederationRepository) InsertInvite(ctx context.Context, inv *federation.Invite) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertInvite(ctx, sqlc.InsertInviteParams{
			ID:              inv.ID,
			Code:            inv.Code,
			CreatedByUserID: inv.CreatedByUserID,
			CreatedAt:       inv.CreatedAt,
			ExpiresAt:       inv.ExpiresAt,
		})
	} else {
		err = r.pq.InsertInvite(ctx, sqlc_pg.InsertInviteParams{
			ID:              inv.ID,
			Code:            inv.Code,
			CreatedByUserID: inv.CreatedByUserID,
			CreatedAt:       inv.CreatedAt,
			ExpiresAt:       inv.ExpiresAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	return nil
}

func (r *FederationRepository) GetInviteByCode(ctx context.Context, code string) (*federation.Invite, error) {
	if r.useSQLite() {
		row, err := r.sq.GetInviteByCode(ctx, code)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get invite by code: %w", err)
		}
		return inviteFromSqliteRow(row), nil
	}
	row, err := r.pq.GetInviteByCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invite by code: %w", err)
	}
	return inviteFromPgRow(row), nil
}

func (r *FederationRepository) MarkInviteUsed(ctx context.Context, inviteID, peerID string, at time.Time) error {
	var err error
	if r.useSQLite() {
		err = r.sq.MarkInviteUsed(ctx, sqlc.MarkInviteUsedParams{
			AcceptedByPeerID: sql.NullString{String: peerID, Valid: peerID != ""},
			AcceptedAt:       sql.NullTime{Time: at, Valid: true},
			ID:               inviteID,
		})
	} else {
		err = r.pq.MarkInviteUsed(ctx, sqlc_pg.MarkInviteUsedParams{
			AcceptedByPeerID: sql.NullString{String: peerID, Valid: peerID != ""},
			AcceptedAt:       sql.NullTime{Time: at, Valid: true},
			ID:               inviteID,
		})
	}
	if err != nil {
		return fmt.Errorf("mark invite used: %w", err)
	}
	return nil
}

func (r *FederationRepository) ListActiveInvites(ctx context.Context) ([]*federation.Invite, error) {
	now := time.Now().UTC()
	if r.useSQLite() {
		rows, err := r.sq.ListActiveInvites(ctx, now)
		if err != nil {
			return nil, fmt.Errorf("list active invites: %w", err)
		}
		out := make([]*federation.Invite, 0, len(rows))
		for i := range rows {
			out = append(out, inviteFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListActiveInvites(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("list active invites: %w", err)
	}
	out := make([]*federation.Invite, 0, len(rows))
	for i := range rows {
		out = append(out, inviteFromPgRow(rows[i]))
	}
	return out, nil
}

func inviteFromSqliteRow(row sqlc.FederationInvite) *federation.Invite {
	inv := &federation.Invite{
		ID:              row.ID,
		Code:            row.Code,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
	if row.AcceptedByPeerID.Valid {
		v := row.AcceptedByPeerID.String
		inv.AcceptedByPeerID = &v
	}
	if row.AcceptedAt.Valid {
		t := row.AcceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv
}

func inviteFromPgRow(row sqlc_pg.FederationInvite) *federation.Invite {
	inv := &federation.Invite{
		ID:              row.ID,
		Code:            row.Code,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
	if row.AcceptedByPeerID.Valid {
		v := row.AcceptedByPeerID.String
		inv.AcceptedByPeerID = &v
	}
	if row.AcceptedAt.Valid {
		t := row.AcceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv
}

// ─── peers ──────────────────────────────────────────────────────────

func (r *FederationRepository) InsertPeer(ctx context.Context, p *federation.Peer) error {
	pairedAt := sql.NullTime{}
	if p.PairedAt != nil {
		pairedAt = sql.NullTime{Time: *p.PairedAt, Valid: true}
	}
	var err error
	if r.useSQLite() {
		err = r.sq.InsertPeer(ctx, sqlc.InsertPeerParams{
			ID:         p.ID,
			ServerUuid: p.ServerUUID,
			Name:       p.Name,
			BaseUrl:    p.BaseURL,
			PublicKey:  []byte(p.PublicKey),
			Status:     string(p.Status),
			CreatedAt:  p.CreatedAt,
			PairedAt:   pairedAt,
		})
	} else {
		err = r.pq.InsertPeer(ctx, sqlc_pg.InsertPeerParams{
			ID:         p.ID,
			ServerUuid: p.ServerUUID,
			Name:       p.Name,
			BaseUrl:    p.BaseURL,
			PublicKey:  []byte(p.PublicKey),
			Status:     string(p.Status),
			CreatedAt:  p.CreatedAt,
			PairedAt:   pairedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert peer: %w", err)
	}
	return nil
}

func (r *FederationRepository) UpdatePeerPaired(ctx context.Context, peerID string, at time.Time) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpdatePeerPaired(ctx, sqlc.UpdatePeerPairedParams{
			PairedAt: sql.NullTime{Time: at, Valid: true},
			ID:       peerID,
		})
	} else {
		err = r.pq.UpdatePeerPaired(ctx, sqlc_pg.UpdatePeerPairedParams{
			PairedAt: sql.NullTime{Time: at, Valid: true},
			ID:       peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer paired: %w", err)
	}
	return nil
}

func (r *FederationRepository) UpdatePeerRevoked(ctx context.Context, peerID string, at time.Time) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.UpdatePeerRevoked(ctx, sqlc.UpdatePeerRevokedParams{
			RevokedAt: sql.NullTime{Time: at, Valid: true},
			ID:        peerID,
		})
	} else {
		n, err = r.pq.UpdatePeerRevoked(ctx, sqlc_pg.UpdatePeerRevokedParams{
			RevokedAt: sql.NullTime{Time: at, Valid: true},
			ID:        peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer revoked: %w", err)
	}
	if n == 0 {
		// Either no such peer or the row was already revoked.
		// Surface the missing-peer case so the manager can propagate
		// a clean 404; an already-revoked re-revoke is rare enough
		// to not warrant a separate sentinel.
		return domain.ErrPeerNotFound
	}
	return nil
}

func (r *FederationRepository) UpdatePeerLastSeen(ctx context.Context, peerID string, at time.Time, statusCode int) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpdatePeerLastSeen(ctx, sqlc.UpdatePeerLastSeenParams{
			LastSeenAt:         sql.NullTime{Time: at, Valid: true},
			LastSeenStatusCode: sql.NullInt64{Int64: int64(statusCode), Valid: true},
			ID:                 peerID,
		})
	} else {
		err = r.pq.UpdatePeerLastSeen(ctx, sqlc_pg.UpdatePeerLastSeenParams{
			LastSeenAt:         sql.NullTime{Time: at, Valid: true},
			LastSeenStatusCode: sql.NullInt32{Int32: int32(statusCode), Valid: true},
			ID:                 peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer last seen: %w", err)
	}
	return nil
}

func (r *FederationRepository) GetPeerByID(ctx context.Context, id string) (*federation.Peer, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPeerByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPeerNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get peer by id: %w", err)
		}
		return peerFromSqliteRow(row), nil
	}
	row, err := r.pq.GetPeerByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get peer by id: %w", err)
	}
	return peerFromPgRow(row), nil
}

func (r *FederationRepository) GetPeerByServerUUID(ctx context.Context, serverUUID string) (*federation.Peer, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPeerByServerUUID(ctx, serverUUID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPeerNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get peer by server uuid: %w", err)
		}
		return peerFromSqliteRow(row), nil
	}
	row, err := r.pq.GetPeerByServerUUID(ctx, serverUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get peer by server uuid: %w", err)
	}
	return peerFromPgRow(row), nil
}

func (r *FederationRepository) ListPeers(ctx context.Context) ([]*federation.Peer, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListPeers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list peers: %w", err)
		}
		out := make([]*federation.Peer, 0, len(rows))
		for i := range rows {
			out = append(out, peerFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	out := make([]*federation.Peer, 0, len(rows))
	for i := range rows {
		out = append(out, peerFromPgRow(rows[i]))
	}
	return out, nil
}

func peerFromSqliteRow(row sqlc.FederationPeer) *federation.Peer {
	p := &federation.Peer{
		ID:         row.ID,
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		BaseURL:    row.BaseUrl,
		PublicKey:  row.PublicKey,
		Status:     federation.PeerStatus(row.Status),
		CreatedAt:  row.CreatedAt,
	}
	if row.PairedAt.Valid {
		t := row.PairedAt.Time
		p.PairedAt = &t
	}
	if row.LastSeenAt.Valid {
		t := row.LastSeenAt.Time
		p.LastSeenAt = &t
	}
	if row.LastSeenStatusCode.Valid {
		v := int(row.LastSeenStatusCode.Int64)
		p.LastSeenStatusCode = &v
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		p.RevokedAt = &t
	}
	return p
}

func peerFromPgRow(row sqlc_pg.FederationPeer) *federation.Peer {
	p := &federation.Peer{
		ID:         row.ID,
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		BaseURL:    row.BaseUrl,
		PublicKey:  row.PublicKey,
		Status:     federation.PeerStatus(row.Status),
		CreatedAt:  row.CreatedAt,
	}
	if row.PairedAt.Valid {
		t := row.PairedAt.Time
		p.PairedAt = &t
	}
	if row.LastSeenAt.Valid {
		t := row.LastSeenAt.Time
		p.LastSeenAt = &t
	}
	if row.LastSeenStatusCode.Valid {
		v := int(row.LastSeenStatusCode.Int32)
		p.LastSeenStatusCode = &v
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		p.RevokedAt = &t
	}
	return p
}

// ─── audit log ──────────────────────────────────────────────────────

func (r *FederationRepository) InsertAuditEntry(ctx context.Context, e *federation.AuditEntry) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertFederationAuditEntry(ctx, sqlc.InsertFederationAuditEntryParams{
			PeerID:       e.PeerID,
			RemoteUserID: nullableString(e.RemoteUserID),
			Method:       e.Method,
			Endpoint:     e.Endpoint,
			StatusCode:   int64(e.StatusCode),
			BytesOut:     e.BytesOut,
			ItemID:       nullableString(e.ItemID),
			SessionID:    nullableString(e.SessionID),
			ErrorKind:    nullableString(e.ErrorKind),
			DurationMs:   sql.NullInt64{Int64: e.DurationMs, Valid: true},
			OccurredAt:   e.OccurredAt,
		})
	} else {
		err = r.pq.InsertFederationAuditEntry(ctx, sqlc_pg.InsertFederationAuditEntryParams{
			PeerID:       e.PeerID,
			RemoteUserID: nullableString(e.RemoteUserID),
			Method:       e.Method,
			Endpoint:     e.Endpoint,
			StatusCode:   int32(e.StatusCode),
			BytesOut:     e.BytesOut,
			ItemID:       nullableString(e.ItemID),
			SessionID:    nullableString(e.SessionID),
			ErrorKind:    nullableString(e.ErrorKind),
			DurationMs:   sql.NullInt32{Int32: int32(e.DurationMs), Valid: true},
			OccurredAt:   e.OccurredAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

func (r *FederationRepository) ListAuditEntries(ctx context.Context, peerID string, limit int) ([]*federation.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if r.useSQLite() {
		rows, err := r.sq.ListFederationAuditEntries(ctx, sqlc.ListFederationAuditEntriesParams{
			PeerID: peerID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list audit entries: %w", err)
		}
		out := make([]*federation.AuditEntry, 0, len(rows))
		for _, row := range rows {
			e := &federation.AuditEntry{
				PeerID:     row.PeerID,
				Method:     row.Method,
				Endpoint:   row.Endpoint,
				StatusCode: int(row.StatusCode),
				BytesOut:   row.BytesOut,
				OccurredAt: row.OccurredAt,
			}
			if row.RemoteUserID.Valid {
				e.RemoteUserID = row.RemoteUserID.String
			}
			if row.ItemID.Valid {
				e.ItemID = row.ItemID.String
			}
			if row.SessionID.Valid {
				e.SessionID = row.SessionID.String
			}
			if row.ErrorKind.Valid {
				e.ErrorKind = row.ErrorKind.String
			}
			if row.DurationMs.Valid {
				e.DurationMs = row.DurationMs.Int64
			}
			out = append(out, e)
		}
		return out, nil
	}
	rows, err := r.pq.ListFederationAuditEntries(ctx, sqlc_pg.ListFederationAuditEntriesParams{
		PeerID: peerID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	out := make([]*federation.AuditEntry, 0, len(rows))
	for _, row := range rows {
		e := &federation.AuditEntry{
			PeerID:     row.PeerID,
			Method:     row.Method,
			Endpoint:   row.Endpoint,
			StatusCode: int(row.StatusCode),
			BytesOut:   row.BytesOut,
			OccurredAt: row.OccurredAt,
		}
		if row.RemoteUserID.Valid {
			e.RemoteUserID = row.RemoteUserID.String
		}
		if row.ItemID.Valid {
			e.ItemID = row.ItemID.String
		}
		if row.SessionID.Valid {
			e.SessionID = row.SessionID.String
		}
		if row.ErrorKind.Valid {
			e.ErrorKind = row.ErrorKind.String
		}
		if row.DurationMs.Valid {
			e.DurationMs = int64(row.DurationMs.Int32)
		}
		out = append(out, e)
	}
	return out, nil
}

// ─── library shares ────────────────────────────────────────────────

func (r *FederationRepository) UpsertLibraryShare(ctx context.Context, s *federation.LibraryShare) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertLibraryShare(ctx, sqlc.UpsertLibraryShareParams{
			ID:          s.ID,
			PeerID:      s.PeerID,
			LibraryID:   s.LibraryID,
			CanBrowse:   s.CanBrowse,
			CanPlay:     s.CanPlay,
			CanDownload: s.CanDownload,
			CanLivetv:   s.CanLiveTV,
			ExtraScopes: nullableString(s.ExtraScopes),
			CreatedBy:   s.CreatedByUserID,
			CreatedAt:   s.CreatedAt,
		})
	} else {
		err = r.pq.UpsertLibraryShare(ctx, sqlc_pg.UpsertLibraryShareParams{
			ID:          s.ID,
			PeerID:      s.PeerID,
			LibraryID:   s.LibraryID,
			CanBrowse:   s.CanBrowse,
			CanPlay:     s.CanPlay,
			CanDownload: s.CanDownload,
			CanLivetv:   s.CanLiveTV,
			ExtraScopes: nullableString(s.ExtraScopes),
			CreatedBy:   s.CreatedByUserID,
			CreatedAt:   s.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert library share: %w", err)
	}
	return nil
}

func (r *FederationRepository) DeleteLibraryShare(ctx context.Context, peerID, shareID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteLibraryShare(ctx, sqlc.DeleteLibraryShareParams{
			PeerID: peerID,
			ID:     shareID,
		})
	} else {
		err = r.pq.DeleteLibraryShare(ctx, sqlc_pg.DeleteLibraryShareParams{
			PeerID: peerID,
			ID:     shareID,
		})
	}
	if err != nil {
		return fmt.Errorf("delete library share: %w", err)
	}
	return nil
}

func (r *FederationRepository) GetLibraryShare(ctx context.Context, peerID, libraryID string) (*federation.LibraryShare, error) {
	if r.useSQLite() {
		row, err := r.sq.GetLibraryShare(ctx, sqlc.GetLibraryShareParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get library share: %w", err)
		}
		return libraryShareFromSqliteRow(row), nil
	}
	row, err := r.pq.GetLibraryShare(ctx, sqlc_pg.GetLibraryShareParams{
		PeerID:    peerID,
		LibraryID: libraryID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get library share: %w", err)
	}
	return libraryShareFromPgRow(row), nil
}

func (r *FederationRepository) ListSharesByPeer(ctx context.Context, peerID string) ([]*federation.LibraryShare, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSharesByPeer(ctx, peerID)
		if err != nil {
			return nil, fmt.Errorf("list shares by peer: %w", err)
		}
		out := make([]*federation.LibraryShare, 0, len(rows))
		for i := range rows {
			out = append(out, libraryShareFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListSharesByPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shares by peer: %w", err)
	}
	out := make([]*federation.LibraryShare, 0, len(rows))
	for i := range rows {
		out = append(out, libraryShareFromPgRow(rows[i]))
	}
	return out, nil
}

func libraryShareFromSqliteRow(row sqlc.FederationLibraryShare) *federation.LibraryShare {
	s := &federation.LibraryShare{
		ID:              row.ID,
		PeerID:          row.PeerID,
		LibraryID:       row.LibraryID,
		CanBrowse:       row.CanBrowse,
		CanPlay:         row.CanPlay,
		CanDownload:     row.CanDownload,
		CanLiveTV:       row.CanLivetv,
		CreatedByUserID: row.CreatedBy,
		CreatedAt:       row.CreatedAt,
	}
	if row.ExtraScopes.Valid {
		s.ExtraScopes = row.ExtraScopes.String
	}
	return s
}

func libraryShareFromPgRow(row sqlc_pg.FederationLibraryShare) *federation.LibraryShare {
	s := &federation.LibraryShare{
		ID:              row.ID,
		PeerID:          row.PeerID,
		LibraryID:       row.LibraryID,
		CanBrowse:       row.CanBrowse,
		CanPlay:         row.CanPlay,
		CanDownload:     row.CanDownload,
		CanLiveTV:       row.CanLivetv,
		CreatedByUserID: row.CreatedBy,
		CreatedAt:       row.CreatedAt,
	}
	if row.ExtraScopes.Valid {
		s.ExtraScopes = row.ExtraScopes.String
	}
	return s
}

func (r *FederationRepository) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*federation.SharedLibrary, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSharedLibrariesForPeer(ctx, peerID)
		if err != nil {
			return nil, fmt.Errorf("list shared libraries: %w", err)
		}
		out := make([]*federation.SharedLibrary, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedLibrary{
				ID:          row.ID,
				Name:        row.Name,
				ContentType: row.ContentType,
				Scopes: federation.ShareScopes{
					CanBrowse:   row.CanBrowse,
					CanPlay:     row.CanPlay,
					CanDownload: row.CanDownload,
					CanLiveTV:   row.CanLivetv,
				},
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListSharedLibrariesForPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shared libraries: %w", err)
	}
	out := make([]*federation.SharedLibrary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &federation.SharedLibrary{
			ID:          row.ID,
			Name:        row.Name,
			ContentType: row.ContentType,
			Scopes: federation.ShareScopes{
				CanBrowse:   row.CanBrowse,
				CanPlay:     row.CanPlay,
				CanDownload: row.CanDownload,
				CanLiveTV:   row.CanLivetv,
			},
		})
	}
	return out, nil
}

// ListSharedItems returns shared items in (peer, library), paginated.
// Caller must have already validated the share + scope; the SQL JOIN
// against federation_library_shares is defence in depth.
//
// PosterColor / PosterColorMuted are attached via a separate batch
// query rather than baked into the sqlc ListSharedItems statement —
// the sqlc 1.31.1 parser truncates `ORDER BY ... COLLATE NOCASE` on
// queries with extra COALESCE-over-subquery columns (see
// architecture-decisions.md). The batch query reads the same
// images.dominant_color* the inline subquery would have, so the
// behaviour is identical: empty strings when the item pre-dates
// migration 014 or extraction failed.
func (r *FederationRepository) ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*federation.SharedItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		total int64
		out   []*federation.SharedItem
		err   error
	)
	if r.useSQLite() {
		total, err = r.sq.CountSharedItems(ctx, sqlc.CountSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("count shared items: %w", err)
		}
		rows, lerr := r.sq.ListSharedItems(ctx, sqlc.ListSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
			Limit:     int64(limit),
			Offset:    int64(offset),
		})
		if lerr != nil {
			return nil, 0, fmt.Errorf("list shared items: %w", lerr)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
			})
		}
	} else {
		total, err = r.pq.CountSharedItems(ctx, sqlc_pg.CountSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("count shared items: %w", err)
		}
		rows, lerr := r.pq.ListSharedItems(ctx, sqlc_pg.ListSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
			Limit:     int32(limit),
			Offset:    int32(offset),
		})
		if lerr != nil {
			return nil, 0, fmt.Errorf("list shared items: %w", lerr)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
			})
		}
	}

	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, 0, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, int(total), nil
}

// ListRecentSharedItems returns the most recently added items across
// every library shared with `peerID` (CanBrowse gate). Powers the
// consumer-side "Recently added on peers" rail: each paired peer
// answers with its top-N freshest titles and the consumer fan-out
// merges them. library_id is included on each row so the consumer
// can route a click into /peers/{peerID}/libraries/{libraryID}/items/{id}.
func (r *FederationRepository) ListRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*federation.SharedItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var out []*federation.SharedItem
	if r.useSQLite() {
		rows, err := r.sq.ListRecentSharedItems(ctx, sqlc.ListRecentSharedItemsParams{
			PeerID: peerID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list recent shared items: %w", err)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
				LibraryID: row.LibraryID,
			})
		}
	} else {
		rows, err := r.pq.ListRecentSharedItems(ctx, sqlc_pg.ListRecentSharedItemsParams{
			PeerID: peerID,
			Limit:  int32(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list recent shared items: %w", err)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
				LibraryID: row.LibraryID,
			})
		}
	}

	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, nil
}

// attachPrimaryImageColors fills SharedItem.PosterColor and
// PosterColorMuted in place by batch-fetching primary-image swatches
// from the images table. Single query for the whole page, so no N+1.
//
// Lives here (raw SQL, not sqlc) because the natural place — a
// correlated subquery on the parent query — trips the sqlc 1.31.1
// parser when combined with ORDER BY ... COLLATE NOCASE. Empty
// `items` and items whose image has no extracted swatch are both
// no-ops (the field already defaults to "").
func (r *FederationRepository) attachPrimaryImageColors(ctx context.Context, items []*federation.SharedItem) error {
	if len(items) == 0 {
		return nil
	}
	placeholders := make([]string, len(items))
	args := make([]any, len(items))
	for i, it := range items {
		placeholders[i] = "?"
		args[i] = it.ID
	}
	// BOOLEAN predicate `is_primary` (truthy) is portable across
	// dialects. The placeholder rewrite has to happen AFTER building
	// the IN list so the counter sees every `?`.
	q := rewritePlaceholders(r.driver, `SELECT item_id, dominant_color, dominant_color_muted
		      FROM images
		      WHERE type = 'primary' AND is_primary
		        AND item_id IN (`+strings.Join(placeholders, ",")+`)`)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	colors := make(map[string][2]string, len(items))
	for rows.Next() {
		var itemID, vibrant, muted string
		if err := rows.Scan(&itemID, &vibrant, &muted); err != nil {
			return err
		}
		colors[itemID] = [2]string{vibrant, muted}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		if c, ok := colors[it.ID]; ok {
			it.PosterColor = c[0]
			it.PosterColorMuted = c[1]
		}
	}
	return nil
}

// SearchSharedItems runs a full-text query across libraries the calling
// peer has CanBrowse on. Reuses the items_fts virtual table (SQLite)
// or the search_vector tsvector column (Postgres) that powers local
// search; ACL gate is the JOIN against federation_library_shares so a
// peer cannot match titles in libraries not shared with them.
//
// Implemented as raw SQL because sqlc parses neither FTS5 MATCH nor
// tsvector @@ to_tsquery. Same precedent as item_repository.go's
// List path. The query parameter is appended with '*' for prefix
// matching (SQLite); the Postgres variant runs through `toTSQueryPrefix`.
//
// Caller is expected to apply a sensible per-peer limit; the function
// caps at 100 defensively so a runaway query cannot stream a peer's
// whole catalog.
func (r *FederationRepository) SearchSharedItems(ctx context.Context, peerID, query string, limit int) ([]*federation.SharedItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if query == "" {
		return []*federation.SharedItem{}, nil
	}

	var ftsParam string
	if r.useSQLite() {
		ftsParam = query + "*"
	} else {
		ftsParam = toTSQueryPrefix(query)
	}

	rows, err := r.db.QueryContext(ctx, r.searchSharedItemsSQL, peerID, ftsParam, limit)
	if err != nil {
		return nil, fmt.Errorf("search shared items: %w", err)
	}
	defer rows.Close()

	out := []*federation.SharedItem{}
	for rows.Next() {
		var it federation.SharedItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.Overview, &it.HasPoster, &it.LibraryID); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		out = append(out, &it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, nil
}

// ─── catalog cache (Phase 4) ────────────────────────────────────────

// UpsertCachedItems replaces all rows for (peer, library) with the
// provided batch in a single transaction. Concurrent readers see
// either the old set or the new set, never half-merged.
//
// Year is dialect-divergent (NullInt64 in SQLite, NullInt32 in
// Postgres) — branching at the bind site keeps the domain field a
// plain `int`.
func (r *FederationRepository) UpsertCachedItems(ctx context.Context, peerID, libraryID string, items []*federation.SharedItem, at time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cache upsert tx: %w", err)
	}
	// Rollback after a successful Commit returns sql.ErrTxDone, which
	// is harmless; ignore it deliberately rather than wrap with
	// extra plumbing.
	defer func() { _ = tx.Rollback() }()

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteCachedItemsForLibrary(ctx, sqlc.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("clear cache: %w", err)
		}
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.DeleteCachedItemsForLibrary(ctx, sqlc_pg.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("clear cache: %w", err)
		}
	}

	for _, it := range items {
		// sqlc 1.31.1 truncates the InsertCachedItem statement when
		// the 10+ placeholders combine with adjacent ORDER BY ...
		// COLLATE NOCASE queries in the same file (see
		// architecture-decisions.md). Raw SQL holdout keeps the
		// colour columns flowing without poking the parser bug.
		var yearArg any
		if it.Year != 0 {
			if r.useSQLite() {
				yearArg = sql.NullInt64{Int64: int64(it.Year), Valid: true}
			} else {
				yearArg = sql.NullInt32{Int32: int32(it.Year), Valid: true}
			}
		} else {
			if r.useSQLite() {
				yearArg = sql.NullInt64{}
			} else {
				yearArg = sql.NullInt32{}
			}
		}
		_, err := tx.ExecContext(ctx, r.insertCachedItemSQL,
			peerID, libraryID, it.ID, it.Type, it.Title,
			yearArg,
			nullableString(it.Overview),
			it.HasPoster,
			it.PosterColor, it.PosterColorMuted,
			at,
		)
		if err != nil {
			return fmt.Errorf("insert cached item %s: %w", it.ID, err)
		}
	}
	return tx.Commit()
}

// ListCachedItems reads the cache for (peer, library), paginated.
// Returns items, total (unpaginated count), and the freshest cached_at
// across rows. Empty result is NOT an error — cache cold.
func (r *FederationRepository) ListCachedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*federation.SharedItem, int, time.Time, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		total      int64
		newestCa   any
		err        error
	)
	if r.useSQLite() {
		header, herr := r.sq.CountAndNewestCachedItems(ctx, sqlc.CountAndNewestCachedItemsParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if herr != nil {
			return nil, 0, time.Time{}, fmt.Errorf("count cached items: %w", herr)
		}
		total, newestCa = header.Total, header.NewestCachedAt
	} else {
		header, herr := r.pq.CountAndNewestCachedItems(ctx, sqlc_pg.CountAndNewestCachedItemsParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if herr != nil {
			return nil, 0, time.Time{}, fmt.Errorf("count cached items: %w", herr)
		}
		total, newestCa = header.Total, header.NewestCachedAt
	}
	if total == 0 {
		return []*federation.SharedItem{}, 0, time.Time{}, nil
	}

	// Raw SQL mirrors UpsertCachedItems: the sqlc ListCachedItems
	// query stays on the previously-working column set, and the
	// two colour columns ride a sibling SELECT here. Same reasoning
	// as in UpsertCachedItems re: the sqlc 1.31.1 parser bug. The
	// `COLLATE NOCASE`-style sort is dialect-substituted at
	// construction time via `caseInsensitiveSort`.
	rows, err := r.db.QueryContext(ctx, r.listCachedItemsSQL, peerID, libraryID, limit, offset)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("list cached items: %w", err)
	}
	defer rows.Close()
	out := []*federation.SharedItem{}
	for rows.Next() {
		var it federation.SharedItem
		if err := rows.Scan(
			&it.ID, &it.Type, &it.Title,
			&it.Year, &it.Overview,
			&it.HasPoster, &it.PosterColor, &it.PosterColorMuted,
		); err != nil {
			return nil, 0, time.Time{}, fmt.Errorf("scan cached item: %w", err)
		}
		out = append(out, &it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, time.Time{}, err
	}
	// MAX(cached_at) is typed as interface{} by sqlc because the
	// aggregate could legitimately return NULL; coerce defensively.
	cachedAt := time.Time{}
	if t, ok := newestCa.(time.Time); ok {
		cachedAt = t
	}
	return out, int(total), cachedAt, nil
}

func (r *FederationRepository) PurgeCachedItemsForLibrary(ctx context.Context, peerID, libraryID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteCachedItemsForLibrary(ctx, sqlc.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
	} else {
		err = r.pq.DeleteCachedItemsForLibrary(ctx, sqlc_pg.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
	}
	if err != nil {
		return fmt.Errorf("purge cache: %w", err)
	}
	return nil
}

// PruneAuditBefore deletes audit rows older than the cutoff and
// returns the number of rows removed. Called from a background
// pruner (Phase 7+); for now it exists for completeness.
func (r *FederationRepository) PruneAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.PruneFederationAuditBefore(ctx, cutoff)
	} else {
		n, err = r.pq.PruneFederationAuditBefore(ctx, cutoff)
	}
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	return n, nil
}

// ─── federation_progress (028) ────────────────────────────────────────────

// UpsertProgress writes or replaces the user's playback state for a
// (peer, remote_item) pair. Duration is preserved across updates that
// pass 0 -- see the SQL upsert for the rationale.
func (r *FederationRepository) UpsertProgress(ctx context.Context, p *federation.Progress) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertFederationProgress(ctx, sqlc.UpsertFederationProgressParams{
			UserID:        p.UserID,
			PeerID:        p.PeerID,
			RemoteItemID:  p.RemoteItemID,
			PositionTicks: p.PositionTicks,
			DurationTicks: p.DurationTicks,
			Completed:     p.Completed,
			LastPlayedAt:  p.LastPlayedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	} else {
		err = r.pq.UpsertFederationProgress(ctx, sqlc_pg.UpsertFederationProgressParams{
			UserID:        p.UserID,
			PeerID:        p.PeerID,
			RemoteItemID:  p.RemoteItemID,
			PositionTicks: p.PositionTicks,
			DurationTicks: p.DurationTicks,
			Completed:     p.Completed,
			LastPlayedAt:  p.LastPlayedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert federation progress: %w", err)
	}
	return nil
}

// GetProgress returns nil, nil when there's no row -- the player
// treats that as "start from 0" with no special-casing.
func (r *FederationRepository) GetProgress(ctx context.Context, userID, peerID, remoteItemID string) (*federation.Progress, error) {
	if r.useSQLite() {
		row, err := r.sq.GetFederationProgress(ctx, sqlc.GetFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, fmt.Errorf("get federation progress: %w", err)
		}
		return &federation.Progress{
			UserID:        row.UserID,
			PeerID:        row.PeerID,
			RemoteItemID:  row.RemoteItemID,
			PositionTicks: row.PositionTicks,
			DurationTicks: row.DurationTicks,
			Completed:     row.Completed,
			LastPlayedAt:  row.LastPlayedAt,
			UpdatedAt:     row.UpdatedAt,
		}, nil
	}
	row, err := r.pq.GetFederationProgress(ctx, sqlc_pg.GetFederationProgressParams{
		UserID:       userID,
		PeerID:       peerID,
		RemoteItemID: remoteItemID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get federation progress: %w", err)
	}
	return &federation.Progress{
		UserID:        row.UserID,
		PeerID:        row.PeerID,
		RemoteItemID:  row.RemoteItemID,
		PositionTicks: row.PositionTicks,
		DurationTicks: row.DurationTicks,
		Completed:     row.Completed,
		LastPlayedAt:  row.LastPlayedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func (r *FederationRepository) DeleteProgress(ctx context.Context, userID, peerID, remoteItemID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteFederationProgress(ctx, sqlc.DeleteFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
	} else {
		err = r.pq.DeleteFederationProgress(ctx, sqlc_pg.DeleteFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
	}
	if err != nil {
		return fmt.Errorf("delete federation progress: %w", err)
	}
	return nil
}

// ListContinueWatching returns the user's in-progress federated items
// ordered by last_played_at desc, joined with the catalog cache for
// title / poster availability. Rows whose cache entry has been evicted
// are dropped silently -- the rail prefers no row over a title-less
// row.
func (r *FederationRepository) ListContinueWatching(ctx context.Context, userID string, limit int) ([]*federation.PeerContinueWatchingItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if r.useSQLite() {
		rows, err := r.sq.ListFederationContinueWatching(ctx, sqlc.ListFederationContinueWatchingParams{
			UserID: userID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list federation continue watching: %w", err)
		}
		out := make([]*federation.PeerContinueWatchingItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.PeerContinueWatchingItem{
				PeerID:        row.PeerID,
				PeerName:      row.PeerName,
				LibraryID:     row.LibraryID,
				RemoteItemID:  row.RemoteItemID,
				Type:          row.Type,
				Title:         row.Title,
				Year:          int(row.Year),
				Overview:      row.Overview,
				HasPoster:     row.HasPoster,
				PositionTicks: row.PositionTicks,
				DurationTicks: row.DurationTicks,
				LastPlayedAt:  row.LastPlayedAt,
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListFederationContinueWatching(ctx, sqlc_pg.ListFederationContinueWatchingParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list federation continue watching: %w", err)
	}
	out := make([]*federation.PeerContinueWatchingItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, &federation.PeerContinueWatchingItem{
			PeerID:        row.PeerID,
			PeerName:      row.PeerName,
			LibraryID:     row.LibraryID,
			RemoteItemID:  row.RemoteItemID,
			Type:          row.Type,
			Title:         row.Title,
			Year:          int(row.Year),
			Overview:      row.Overview,
			HasPoster:     row.HasPoster,
			PositionTicks: row.PositionTicks,
			DurationTicks: row.DurationTicks,
			LastPlayedAt:  row.LastPlayedAt,
		})
	}
	return out, nil
}
