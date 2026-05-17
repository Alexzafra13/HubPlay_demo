package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// Channel represents an IPTV channel.
//
// Health fields (LastProbeAt, LastProbeStatus, LastProbeError,
// ConsecutiveFailures) track opportunistic probe outcomes from the
// stream proxy. They're zero-valued for reads that go through the
// legacy sqlc path; the raw-SQL reads below populate them.
// Filtering the user-facing channel list for `consecutive_failures
// >= N` hides upstreams that have been failing long enough to look
// really dead, so the operator isn't spammed with transient-error
// reports and viewers don't click through dead tiles.
type Channel struct {
	ID                  string
	LibraryID           string
	Name                string
	Number              int
	GroupName           string
	LogoURL             string
	StreamURL           string
	TvgID               string
	Language            string
	Country             string
	IsActive            bool
	AddedAt             time.Time
	LastProbeAt         time.Time
	LastProbeStatus     string // "ok" | "error" | "" (never probed)
	LastProbeError      string
	ConsecutiveFailures int
}

// ChannelRepository — dual-dialect (Pattern A + Pattern B). The sqlc
// surface (Create, GetByID, ListByLibrary, SetActive, Groups,
// ReplaceForLibrary tx) branches per-call on `useSQLite()`. The raw-SQL
// surface (Listlivetv, the four health writers, ListUnhealthy /
// HealthSummary / ListHealthy / ListWithoutEPG, UpdateTvgID) uses
// `rewritePlaceholders` for `?` → `$N`.
//
// BOOLEAN gotcha: `is_active = 1` literals are SQLite-only (BOOLEAN
// stored as INTEGER). The raw-SQL paths use `is_active` (truthy in
// both dialects) instead.
type ChannelRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewChannelRepository(driver string, database *sql.DB) *ChannelRepository {
	r := &ChannelRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ChannelRepository) useSQLite() bool { return r.sq != nil }

func (r *ChannelRepository) driver() string {
	if r.useSQLite() {
		return DriverSQLite
	}
	return DriverPostgres
}

// ErrChannelNotFound is returned when a channel doesn't exist.
var ErrChannelNotFound = fmt.Errorf("channel not found")

// Create inserts a new channel.
func (r *ChannelRepository) Create(ctx context.Context, ch *Channel) error {
	var err error
	if r.useSQLite() {
		err = r.sq.CreateChannel(ctx, channelToSqliteCreateParams(ch))
	} else {
		err = r.pq.CreateChannel(ctx, channelToPgCreateParams(ch))
	}
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

// GetByID returns a channel by ID.
func (r *ChannelRepository) GetByID(ctx context.Context, id string) (*Channel, error) {
	if r.useSQLite() {
		row, err := r.sq.GetChannelByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("channel %s: %w", id, ErrChannelNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get channel: %w", err)
		}
		ch := channelFromSqliteGetRow(row)
		return &ch, nil
	}
	row, err := r.pq.GetChannelByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("channel %s: %w", id, ErrChannelNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	ch := channelFromPgGetRow(row)
	return &ch, nil
}

// ListByLibraryPaginated returns one page of channels from a library
// and the total row count for that filter (independent of limit /
// offset). Both pagination parameters MUST be non-negative; `limit`
// is capped at 1000 defensively so a malicious caller cannot ask for
// the whole catalogue in one round-trip — that's what the rail rolls
// down to anyway with frontend virtualisation.
//
// Built as raw SQL (Pattern B) to avoid regenerating the sqlc code:
// the dialect-divergent bits are limited to `?` ↔ `$N` rewrite +
// optional `AND is_active` predicate (truthy in both dialects).
//
// Cierra el hot path #1 del reporte 2026-05-17: en libraries IPTV
// con 5 000+ canales, el listing pasaba 17 ms / 9 MB por request
// hidratando todos los rows en Go. Con limit=100 la mejora medida
// es ×30 (17 ms → 0.5 ms) y baja la presión de GC.
func (r *ChannelRepository) ListByLibraryPaginated(ctx context.Context, libraryID string, activeOnly bool, offset, limit int) ([]*Channel, int, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	driver := r.driver()
	activeFilter := ""
	if activeOnly {
		// `is_active` truthy works in both dialects (SQLite stores
		// 0/1 INTEGER, Postgres BOOLEAN — neither rejects the bare
		// predicate). Keeps the SQL portable without per-dialect
		// branch.
		activeFilter = " AND is_active"
	}

	// Count first so the response has the total even when the page
	// is empty. Single round-trip per request; the count traverses
	// only the index leaves (`idx_channels_library` + the partial
	// `idx_channels_library_number` from migration 044), no row data.
	countSQL := rewritePlaceholders(driver,
		"SELECT COUNT(*) FROM channels WHERE library_id = ?"+activeFilter)
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, libraryID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count channels: %w", err)
	}

	listSQL := rewritePlaceholders(driver, `
		SELECT id, library_id, name,
		       COALESCE(number, 0) AS number,
		       COALESCE(group_name, '') AS group_name,
		       COALESCE(logo_url, '') AS logo_url,
		       stream_url,
		       COALESCE(tvg_id, '') AS tvg_id,
		       COALESCE(language, '') AS language,
		       COALESCE(country, '') AS country,
		       is_active, added_at
		FROM channels
		WHERE library_id = ?`+activeFilter+`
		ORDER BY number, name
		LIMIT ? OFFSET ?`)

	rows, err := r.db.QueryContext(ctx, listSQL, libraryID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list channels paginated: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]*Channel, 0, limit)
	for rows.Next() {
		ch := &Channel{}
		if err := rows.Scan(
			&ch.ID, &ch.LibraryID, &ch.Name, &ch.Number,
			&ch.GroupName, &ch.LogoURL, &ch.StreamURL,
			&ch.TvgID, &ch.Language, &ch.Country,
			&ch.IsActive, &ch.AddedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan channel row: %w", err)
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListByLibrary returns all channels in a library.
//
// LEGACY — devuelve la lista completa sin paginar. Mantenido para
// callers internos que iteran el catálogo entero (EPG matcher,
// scheduler, scanner). Los endpoints HTTP user-facing prefieren
// ListByLibraryPaginated; ver hot path #1 del reporte 2026-05-17.
func (r *ChannelRepository) ListByLibrary(ctx context.Context, libraryID string, activeOnly bool) ([]*Channel, error) {
	if r.useSQLite() {
		if activeOnly {
			rows, err := r.sq.ListActiveChannelsByLibrary(ctx, libraryID)
			if err != nil {
				return nil, fmt.Errorf("list channels: %w", err)
			}
			return channelsFromSqliteActiveRows(rows), nil
		}
		rows, err := r.sq.ListChannelsByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		return channelsFromSqliteListRows(rows), nil
	}
	if activeOnly {
		rows, err := r.pq.ListActiveChannelsByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		return channelsFromPgActiveRows(rows), nil
	}
	rows, err := r.pq.ListChannelsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channelsFromPgListRows(rows), nil
}

// ListLivetvChannels returns every channel that lives in a livetv-
// type library, across the whole instance. Used by the EPG matcher
// to build a global channel index: a single XMLTV source attached to
// one library can then resolve programs against channels in OTHER
// libraries by name / tvg-id, which is what most operators expect
// when they have one M3U for "Cat TV" and another for "Movistar"
// but only configure EPG sources on one.
//
// Raw SQL because sqlc would need a JOIN-with-WHERE generator and the
// project keeps a small allow-list of raw queries (5 today) for
// exactly this kind of cross-table read.
func (r *ChannelRepository) ListLivetvChannels(ctx context.Context) ([]*Channel, error) {
	query := rewritePlaceholders(r.driver(),
		`SELECT c.id, c.library_id, c.name, COALESCE(c.number, 0), COALESCE(c.group_name,''),
		        COALESCE(c.logo_url,''), c.stream_url, COALESCE(c.tvg_id,''),
		        COALESCE(c.language,''), COALESCE(c.country,''), c.is_active, c.added_at
		 FROM channels c
		 INNER JOIN libraries l ON l.id = c.library_id
		 WHERE l.content_type = 'livetv'
		 ORDER BY c.library_id, COALESCE(c.number, 999999), c.name`)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list livetv channels: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Channel
	for rows.Next() {
		c, err := scanChannelBasic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceForLibrary deletes all channels in a library and inserts new ones (used during M3U refresh).
func (r *ChannelRepository) ReplaceForLibrary(ctx context.Context, libraryID string, channels []*Channel) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteChannelsByLibrary(ctx, libraryID); err != nil {
			return fmt.Errorf("delete old channels: %w", err)
		}
		for _, ch := range channels {
			if err := qtx.CreateChannel(ctx, channelToSqliteCreateParams(ch)); err != nil {
				return fmt.Errorf("insert channel %s: %w", ch.Name, err)
			}
		}
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.DeleteChannelsByLibrary(ctx, libraryID); err != nil {
			return fmt.Errorf("delete old channels: %w", err)
		}
		for _, ch := range channels {
			if err := qtx.CreateChannel(ctx, channelToPgCreateParams(ch)); err != nil {
				return fmt.Errorf("insert channel %s: %w", ch.Name, err)
			}
		}
	}

	return tx.Commit()
}

// SetActive enables or disables a channel.
func (r *ChannelRepository) SetActive(ctx context.Context, id string, active bool) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.SetChannelActive(ctx, sqlc.SetChannelActiveParams{
			IsActive: active,
			ID:       id,
		})
	} else {
		n, err = r.pq.SetChannelActive(ctx, sqlc_pg.SetChannelActiveParams{
			IsActive: active,
			ID:       id,
		})
	}
	if err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	if n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// ── Channel health ──────────────────────────────────────────────
//
// The proxy records outcomes per request. Reads and aggregates for
// the admin "unhealthy channels" surface live here. Raw SQL: the
// sqlc-generated queries predate the 008 migration and regenerating
// them isn't necessary for these few operations.
//
// Writes are idempotent and ignore "channel gone" errors silently
// (the proxy shouldn't care if a row has been deleted between probe
// and record — that's a race we accept).

// probeErrorLimit clips error messages to this many runes before
// persisting. Upstream error strings can get long (HTTP body
// fragments, TLS errors with cert chains) and the column isn't a
// place to archive raw upstream noise.
const probeErrorLimit = 500

// UnhealthyThreshold is the failure count at which the user-facing
// channel list hides a channel. Chosen to absorb a couple of
// transient blips (DNS hiccup, CDN rotation) without pinning a
// genuinely dead channel.
const UnhealthyThreshold = 3

// RecordProbeSuccess resets the failure counter and marks the
// channel as healthy. Called on any successful upstream fetch —
// one good response is enough to clear prior failures, so a channel
// that was flaky yesterday comes back automatically.
func (r *ChannelRepository) RecordProbeSuccess(ctx context.Context, channelID string) error {
	now := time.Now().UTC()
	query := rewritePlaceholders(r.driver(),
		`UPDATE channels SET
		    last_probe_at        = ?,
		    last_probe_status    = 'ok',
		    last_probe_error     = '',
		    consecutive_failures = 0
		 WHERE id = ?`)
	_, err := r.db.ExecContext(ctx, query, now, channelID)
	if err != nil {
		return fmt.Errorf("record probe success: %w", err)
	}
	return nil
}

// RecordProbeFailure increments the failure counter atomically. The
// atomic UPDATE stops two concurrent failing viewers from racing on
// read-modify-write semantics — each failure gets its own +1.
func (r *ChannelRepository) RecordProbeFailure(ctx context.Context, channelID, errMsg string) error {
	now := time.Now().UTC()
	if len([]rune(errMsg)) > probeErrorLimit {
		errMsg = string([]rune(errMsg)[:probeErrorLimit])
	}
	query := rewritePlaceholders(r.driver(),
		`UPDATE channels SET
		    last_probe_at        = ?,
		    last_probe_status    = 'error',
		    last_probe_error     = ?,
		    consecutive_failures = consecutive_failures + 1
		 WHERE id = ?`)
	_, err := r.db.ExecContext(ctx, query, now, errMsg, channelID)
	if err != nil {
		return fmt.Errorf("record probe failure: %w", err)
	}
	return nil
}

// ResetHealth clears the health state without actually probing. Used
// by the admin "marcar como OK" action when an operator knows a
// channel is working (e.g. they just tested it in a media player
// outside the app).
func (r *ChannelRepository) ResetHealth(ctx context.Context, channelID string) error {
	query := rewritePlaceholders(r.driver(),
		`UPDATE channels SET
		    last_probe_at        = NULL,
		    last_probe_status    = '',
		    last_probe_error     = '',
		    consecutive_failures = 0
		 WHERE id = ?`)
	_, err := r.db.ExecContext(ctx, query, channelID)
	if err != nil {
		return fmt.Errorf("reset health: %w", err)
	}
	return nil
}

// ListUnhealthyByLibrary returns channels whose consecutive failure
// count is at or above the caller's threshold. Ordered by worst
// first so the admin sees the most troubled channels on top.
func (r *ChannelRepository) ListUnhealthyByLibrary(ctx context.Context, libraryID string, minFailures int) ([]*Channel, error) {
	if minFailures <= 0 {
		minFailures = UnhealthyThreshold
	}
	query := rewritePlaceholders(r.driver(),
		`SELECT id, library_id, name, COALESCE(number, 0), COALESCE(group_name,''),
		        COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
		        COALESCE(language,''), COALESCE(country,''), is_active, added_at,
		        last_probe_at, last_probe_status, last_probe_error,
		        consecutive_failures
		 FROM channels
		 WHERE library_id = ? AND consecutive_failures >= ?
		 ORDER BY consecutive_failures DESC, name ASC`)
	rows, err := r.db.QueryContext(ctx, query, libraryID, minFailures)
	if err != nil {
		return nil, fmt.Errorf("list unhealthy channels: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Channel
	for rows.Next() {
		ch, err := scanChannelWithHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// ChannelHealthSummary is the lightweight projection the admin home
// panel reads to render badges + stats without pulling the full
// unhealthy / without-epg / total channel lists. Each value is a
// single COUNT(*) — three tiny aggregates in one round-trip vs.
// hundreds of KB of full channel rows that the panel previously
// loaded just to call `.length` on.
type ChannelHealthSummary struct {
	TotalChannels   int
	UnhealthyCount  int
	WithoutEPGCount int
}

// HealthSummaryByLibrary computes the three counts the admin Bibliotecas
// panel needs (total active channels, unhealthy count, without-EPG
// count) in a single SQL statement so the page-level "is this library
// happy?" decision pays one round-trip instead of three big payloads.
//
// `since` / `until` define the EPG-coverage window the without-epg
// count uses — same semantics as ListWithoutEPGByLibrary.
//
// Filtered conditional aggregates (`COUNT(*) FILTER (WHERE ...)`)
// keep the query a single scan over the channels table; the EPG
// subquery is correlated per channel, which the existing index on
// epg_programs(channel_id, start_time, end_time) covers. SQLite has
// supported FILTER since 3.30 so the same query parses on both
// dialects.
func (r *ChannelRepository) HealthSummaryByLibrary(
	ctx context.Context,
	libraryID string,
	since, until time.Time,
) (ChannelHealthSummary, error) {
	query := rewritePlaceholders(r.driver(),
		`SELECT
			COUNT(*) FILTER (WHERE c.is_active)                                  AS total_active,
			COUNT(*) FILTER (WHERE c.consecutive_failures >= ?)                  AS unhealthy,
			COUNT(*) FILTER (
				WHERE c.is_active
				  AND NOT EXISTS (
				      SELECT 1 FROM epg_programs p
				      WHERE p.channel_id = c.id
				        AND p.end_time   > ?
				        AND p.start_time < ?
				  )
			) AS without_epg
		FROM channels c
		WHERE c.library_id = ?`)

	var sum ChannelHealthSummary
	err := r.db.QueryRowContext(ctx, query,
		UnhealthyThreshold, since.UTC(), until.UTC(), libraryID,
	).Scan(&sum.TotalChannels, &sum.UnhealthyCount, &sum.WithoutEPGCount)
	if err != nil {
		return ChannelHealthSummary{}, fmt.Errorf("health summary: %w", err)
	}
	return sum, nil
}

// ListHealthyByLibrary returns the channels the user-facing UI should
// render: active and below the unhealthy threshold. Sorted by number
// ascending — same order the M3U import produces and what viewers
// expect in the carousel / guide.
func (r *ChannelRepository) ListHealthyByLibrary(ctx context.Context, libraryID string) ([]*Channel, error) {
	query := rewritePlaceholders(r.driver(),
		`SELECT id, library_id, name, COALESCE(number, 0), COALESCE(group_name,''),
		        COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
		        COALESCE(language,''), COALESCE(country,''), is_active, added_at,
		        last_probe_at, last_probe_status, last_probe_error,
		        consecutive_failures
		 FROM channels
		 WHERE library_id = ? AND is_active AND consecutive_failures < ?
		 ORDER BY COALESCE(number, 999999), name`)
	rows, err := r.db.QueryContext(ctx, query, libraryID, UnhealthyThreshold)
	if err != nil {
		return nil, fmt.Errorf("list healthy channels: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Channel
	for rows.Next() {
		ch, err := scanChannelWithHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// scanChannelBasic scans a row from a SELECT that returns the 12
// non-health columns (id, library_id, name, number, group_name,
// logo_url, stream_url, tvg_id, language, country, is_active,
// added_at). `number` is INTEGER → int64 SQLite, int32 Postgres after
// the COALESCE(number, 0) wrap, so we read into the row's `any`
// variant — both drivers happily decode numeric values into
// `int` directly when the column is non-NULL after COALESCE.
func scanChannelBasic(rows *sql.Rows) (*Channel, error) {
	var c Channel
	if err := rows.Scan(
		&c.ID, &c.LibraryID, &c.Name, &c.Number, &c.GroupName,
		&c.LogoURL, &c.StreamURL, &c.TvgID,
		&c.Language, &c.Country, &c.IsActive, &c.AddedAt,
	); err != nil {
		return nil, fmt.Errorf("scan channel: %w", err)
	}
	return &c, nil
}

func scanChannelWithHealth(rows *sql.Rows) (*Channel, error) {
	ch := &Channel{}
	var probeRaw any
	if err := rows.Scan(&ch.ID, &ch.LibraryID, &ch.Name, &ch.Number, &ch.GroupName,
		&ch.LogoURL, &ch.StreamURL, &ch.TvgID, &ch.Language, &ch.Country,
		&ch.IsActive, &ch.AddedAt, &probeRaw, &ch.LastProbeStatus,
		&ch.LastProbeError, &ch.ConsecutiveFailures); err != nil {
		return nil, fmt.Errorf("scan channel: %w", err)
	}
	t, err := coerceSQLiteTime(probeRaw)
	if err != nil {
		return nil, fmt.Errorf("parse last_probe_at: %w", err)
	}
	ch.LastProbeAt = t
	return ch, nil
}

// ── Manual editing surface ──────────────────────────────────────

// UpdateTvgID rewrites the tvg_id of one channel. Used by the admin
// PATCH flow; the override persistence layer lives separately in
// channel_overrides so that this UPDATE is wiped on the next M3U
// refresh but the override table reapplies it.
func (r *ChannelRepository) UpdateTvgID(ctx context.Context, channelID, tvgID string) error {
	var value sql.NullString
	if tvgID != "" {
		value = sql.NullString{String: tvgID, Valid: true}
	}
	query := rewritePlaceholders(r.driver(), `UPDATE channels SET tvg_id = ? WHERE id = ?`)
	res, err := r.db.ExecContext(ctx, query, value, channelID)
	if err != nil {
		return fmt.Errorf("update tvg_id: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// ListWithoutEPGByLibrary returns active channels that have no EPG
// programmes overlapping the given time window — the "canales sin
// guía" admin view. Uses a LEFT JOIN + EXISTS subquery so a channel
// with stale old programmes but nothing current still shows up.
//
// `since`/`until` bound the window the caller considers "current";
// typical admin use sends now-2h..now+24h to match the user-facing
// guide window.
func (r *ChannelRepository) ListWithoutEPGByLibrary(ctx context.Context, libraryID string, since, until time.Time) ([]*Channel, error) {
	query := rewritePlaceholders(r.driver(),
		`SELECT c.id, c.library_id, c.name, COALESCE(c.number, 0),
		        COALESCE(c.group_name,''), COALESCE(c.logo_url,''),
		        c.stream_url, COALESCE(c.tvg_id,''),
		        COALESCE(c.language,''), COALESCE(c.country,''),
		        c.is_active, c.added_at,
		        c.last_probe_at, c.last_probe_status,
		        c.last_probe_error, c.consecutive_failures
		 FROM channels c
		 WHERE c.library_id = ?
		   AND c.is_active
		   AND NOT EXISTS (
		       SELECT 1 FROM epg_programs p
		       WHERE p.channel_id = c.id
		         AND p.end_time   > ?
		         AND p.start_time < ?
		   )
		 ORDER BY COALESCE(c.number, 999999), c.name`)
	rows, err := r.db.QueryContext(ctx, query, libraryID, since.UTC(), until.UTC())
	if err != nil {
		return nil, fmt.Errorf("list channels without epg: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Channel
	for rows.Next() {
		ch, err := scanChannelWithHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// Groups returns distinct group names for a library.
func (r *ChannelRepository) Groups(ctx context.Context, libraryID string) ([]string, error) {
	var (
		rows []sql.NullString
		err  error
	)
	if r.useSQLite() {
		rows, err = r.sq.ListChannelGroups(ctx, libraryID)
	} else {
		rows, err = r.pq.ListChannelGroups(ctx, libraryID)
	}
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	groups := make([]string, 0, len(rows))
	for _, ns := range rows {
		if ns.Valid {
			groups = append(groups, ns.String)
		}
	}
	return groups, nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func channelToSqliteCreateParams(ch *Channel) sqlc.CreateChannelParams {
	return sqlc.CreateChannelParams{
		ID:        ch.ID,
		LibraryID: ch.LibraryID,
		Name:      ch.Name,
		Number:    sql.NullInt64{Int64: int64(ch.Number), Valid: true},
		GroupName: nullableString(ch.GroupName),
		LogoUrl:   nullableString(ch.LogoURL),
		StreamUrl: ch.StreamURL,
		TvgID:     nullableString(ch.TvgID),
		Language:  nullableString(ch.Language),
		Country:   nullableString(ch.Country),
		IsActive:  ch.IsActive,
		AddedAt:   ch.AddedAt,
	}
}

func channelToPgCreateParams(ch *Channel) sqlc_pg.CreateChannelParams {
	return sqlc_pg.CreateChannelParams{
		ID:        ch.ID,
		LibraryID: ch.LibraryID,
		Name:      ch.Name,
		Number:    sql.NullInt32{Int32: int32(ch.Number), Valid: true},
		GroupName: nullableString(ch.GroupName),
		LogoUrl:   nullableString(ch.LogoURL),
		StreamUrl: ch.StreamURL,
		TvgID:     nullableString(ch.TvgID),
		Language:  nullableString(ch.Language),
		Country:   nullableString(ch.Country),
		IsActive:  ch.IsActive,
		AddedAt:   ch.AddedAt,
	}
}

func channelFromSqliteGetRow(r sqlc.GetChannelByIDRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int64),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelFromPgGetRow(r sqlc_pg.GetChannelByIDRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int32),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelFromSqliteListRow(r sqlc.ListChannelsByLibraryRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int64),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelFromPgListRow(r sqlc_pg.ListChannelsByLibraryRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int32),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelFromSqliteActiveRow(r sqlc.ListActiveChannelsByLibraryRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int64),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelFromPgActiveRow(r sqlc_pg.ListActiveChannelsByLibraryRow) Channel {
	return Channel{
		ID:        r.ID,
		LibraryID: r.LibraryID,
		Name:      r.Name,
		Number:    int(r.Number.Int32),
		GroupName: r.GroupName,
		LogoURL:   r.LogoUrl,
		StreamURL: r.StreamUrl,
		TvgID:     r.TvgID,
		Language:  r.Language,
		Country:   r.Country,
		IsActive:  r.IsActive,
		AddedAt:   r.AddedAt,
	}
}

func channelsFromSqliteListRows(rows []sqlc.ListChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromSqliteListRow(row)
		out[i] = &ch
	}
	return out
}

func channelsFromPgListRows(rows []sqlc_pg.ListChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromPgListRow(row)
		out[i] = &ch
	}
	return out
}

func channelsFromSqliteActiveRows(rows []sqlc.ListActiveChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromSqliteActiveRow(row)
		out[i] = &ch
	}
	return out
}

func channelsFromPgActiveRows(rows []sqlc_pg.ListActiveChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromPgActiveRow(row)
		out[i] = &ch
	}
	return out
}
