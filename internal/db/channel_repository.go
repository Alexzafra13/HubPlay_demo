package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
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
	ID                   string
	LibraryID            string
	Name                 string
	Number               int
	GroupName            string
	LogoURL              string
	StreamURL            string
	TvgID                string
	Language             string
	Country              string
	IsActive             bool
	AddedAt              time.Time
	LastProbeAt          time.Time
	LastProbeStatus      string // "ok" | "error" | "" (never probed)
	LastProbeError       string
	ConsecutiveFailures  int
}

type ChannelRepository struct {
	db *sql.DB
	q  *sqlc.Queries
}

func NewChannelRepository(database *sql.DB) *ChannelRepository {
	return &ChannelRepository{db: database, q: sqlc.New(database)}
}

// ErrChannelNotFound is returned when a channel doesn't exist.
var ErrChannelNotFound = fmt.Errorf("channel not found")

// Create inserts a new channel.
func (r *ChannelRepository) Create(ctx context.Context, ch *Channel) error {
	err := r.q.CreateChannel(ctx, channelToCreateParams(ch))
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

// GetByID returns a channel by ID.
func (r *ChannelRepository) GetByID(ctx context.Context, id string) (*Channel, error) {
	row, err := r.q.GetChannelByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("channel %s: %w", id, ErrChannelNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	ch := channelFromGetRow(row)
	return &ch, nil
}

// ListByLibrary returns all channels in a library.
func (r *ChannelRepository) ListByLibrary(ctx context.Context, libraryID string, activeOnly bool) ([]*Channel, error) {
	if activeOnly {
		rows, err := r.q.ListActiveChannelsByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		return channelsFromActiveRows(rows), nil
	}
	rows, err := r.q.ListChannelsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channelsFromListRows(rows), nil
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
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.library_id, c.name, COALESCE(c.number, 0), COALESCE(c.group_name,''),
		        COALESCE(c.logo_url,''), c.stream_url, COALESCE(c.tvg_id,''),
		        COALESCE(c.language,''), COALESCE(c.country,''), c.is_active, c.added_at
		 FROM channels c
		 INNER JOIN libraries l ON l.id = c.library_id
		 WHERE l.content_type = 'livetv'
		 ORDER BY c.library_id, COALESCE(c.number, 999999), c.name`)
	if err != nil {
		return nil, fmt.Errorf("list livetv channels: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(
			&c.ID, &c.LibraryID, &c.Name, &c.Number, &c.GroupName,
			&c.LogoURL, &c.StreamURL, &c.TvgID,
			&c.Language, &c.Country, &c.IsActive, &c.AddedAt,
		); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		out = append(out, &c)
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

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteChannelsByLibrary(ctx, libraryID); err != nil {
		return fmt.Errorf("delete old channels: %w", err)
	}

	for _, ch := range channels {
		if err := qtx.CreateChannel(ctx, channelToCreateParams(ch)); err != nil {
			return fmt.Errorf("insert channel %s: %w", ch.Name, err)
		}
	}

	return tx.Commit()
}

// SetActive enables or disables a channel.
func (r *ChannelRepository) SetActive(ctx context.Context, id string, active bool) error {
	n, err := r.q.SetChannelActive(ctx, sqlc.SetChannelActiveParams{
		IsActive: active,
		ID:       id,
	})
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET
		    last_probe_at        = ?,
		    last_probe_status    = 'ok',
		    last_probe_error     = '',
		    consecutive_failures = 0
		 WHERE id = ?`, now, channelID)
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET
		    last_probe_at        = ?,
		    last_probe_status    = 'error',
		    last_probe_error     = ?,
		    consecutive_failures = consecutive_failures + 1
		 WHERE id = ?`, now, errMsg, channelID)
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET
		    last_probe_at        = NULL,
		    last_probe_status    = '',
		    last_probe_error     = '',
		    consecutive_failures = 0
		 WHERE id = ?`, channelID)
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
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, name, COALESCE(number, 0), COALESCE(group_name,''),
		        COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
		        COALESCE(language,''), COALESCE(country,''), is_active, added_at,
		        COALESCE(last_probe_at, ''), last_probe_status, last_probe_error,
		        consecutive_failures
		 FROM channels
		 WHERE library_id = ? AND consecutive_failures >= ?
		 ORDER BY consecutive_failures DESC, name ASC`,
		libraryID, minFailures)
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

// ListHealthyByLibrary returns the channels the user-facing UI should
// render: active and below the unhealthy threshold. Sorted by number
// ascending — same order the M3U import produces and what viewers
// expect in the carousel / guide.
func (r *ChannelRepository) ListHealthyByLibrary(ctx context.Context, libraryID string) ([]*Channel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, name, COALESCE(number, 0), COALESCE(group_name,''),
		        COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
		        COALESCE(language,''), COALESCE(country,''), is_active, added_at,
		        COALESCE(last_probe_at, ''), last_probe_status, last_probe_error,
		        consecutive_failures
		 FROM channels
		 WHERE library_id = ? AND is_active = 1 AND consecutive_failures < ?
		 ORDER BY COALESCE(number, 999999), name`,
		libraryID, UnhealthyThreshold)
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
	res, err := r.db.ExecContext(ctx,
		`UPDATE channels SET tvg_id = ? WHERE id = ?`, value, channelID)
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
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.library_id, c.name, COALESCE(c.number, 0),
		        COALESCE(c.group_name,''), COALESCE(c.logo_url,''),
		        c.stream_url, COALESCE(c.tvg_id,''),
		        COALESCE(c.language,''), COALESCE(c.country,''),
		        c.is_active, c.added_at,
		        COALESCE(c.last_probe_at, ''), c.last_probe_status,
		        c.last_probe_error, c.consecutive_failures
		 FROM channels c
		 WHERE c.library_id = ?
		   AND c.is_active = 1
		   AND NOT EXISTS (
		       SELECT 1 FROM epg_programs p
		       WHERE p.channel_id = c.id
		         AND p.end_time   > ?
		         AND p.start_time < ?
		   )
		 ORDER BY COALESCE(c.number, 999999), c.name`,
		libraryID, since.UTC(), until.UTC())
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
	rows, err := r.q.ListChannelGroups(ctx, libraryID)
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

func channelToCreateParams(ch *Channel) sqlc.CreateChannelParams {
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

func channelFromGetRow(r sqlc.GetChannelByIDRow) Channel {
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

func channelFromListRow(r sqlc.ListChannelsByLibraryRow) Channel {
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

func channelFromActiveRow(r sqlc.ListActiveChannelsByLibraryRow) Channel {
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

func channelsFromListRows(rows []sqlc.ListChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromListRow(row)
		out[i] = &ch
	}
	return out
}

func channelsFromActiveRows(rows []sqlc.ListActiveChannelsByLibraryRow) []*Channel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Channel, len(rows))
	for i, row := range rows {
		ch := channelFromActiveRow(row)
		out[i] = &ch
	}
	return out
}
