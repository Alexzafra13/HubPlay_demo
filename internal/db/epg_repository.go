package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
)

// EPGProgram represents a TV program in the electronic program guide.
type EPGProgram struct {
	ID          string
	ChannelID   string
	Title       string
	Description string
	Category    string
	IconURL     string
	StartTime   time.Time
	EndTime     time.Time
}

type EPGProgramRepository struct {
	db *sql.DB // kept for BulkSchedule (dynamic IN)
	q  *sqlc.Queries
}

func NewEPGProgramRepository(database *sql.DB) *EPGProgramRepository {
	return &EPGProgramRepository{db: database, q: sqlc.New(database)}
}

// ReplaceForChannel deletes all programs for a channel and inserts new ones.
//
// Start/end times are coerced to UTC before persisting: modernc.org/sqlite
// serialises a `time.Time` whose Location is a named zone via
// `time.Time.String()` — "2026-04-24 12:00:00 +0200 +0200" — which the
// default Scan path cannot parse back into a time.Time. UTC round-trips
// cleanly. XMLTV feeds always carry a zone offset (davidmuma, iptv-org,
// epg.pw) so the raw time from ParseXMLTV would otherwise trip this.
func (r *EPGProgramRepository) ReplaceForChannel(ctx context.Context, channelID string, programs []*EPGProgram) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteEPGProgramsByChannel(ctx, channelID); err != nil {
		return fmt.Errorf("delete old programs: %w", err)
	}

	for _, p := range programs {
		err := qtx.InsertEPGProgram(ctx, sqlc.InsertEPGProgramParams{
			ID:          p.ID,
			ChannelID:   p.ChannelID,
			Title:       p.Title,
			Description: nullableString(p.Description),
			Category:    nullableString(p.Category),
			IconUrl:     nullableString(p.IconURL),
			StartTime:   p.StartTime.UTC(),
			EndTime:     p.EndTime.UTC(),
		})
		if err != nil {
			return fmt.Errorf("insert program: %w", err)
		}
	}

	return tx.Commit()
}

// NowPlaying returns the currently airing program for a channel.
//
// Reads via raw SQL (not sqlc) so the coerce helper can rescue rows
// persisted by older builds in the Go-stringer time format.
func (r *EPGProgramRepository) NowPlaying(ctx context.Context, channelID string) (*EPGProgram, error) {
	now := time.Now().UTC()

	row := r.db.QueryRowContext(ctx,
		`SELECT id, channel_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(icon_url,''), start_time, end_time
		 FROM epg_programs
		 WHERE channel_id = ? AND start_time <= ? AND end_time > ?
		 LIMIT 1`, channelID, now, now)

	p := &EPGProgram{}
	var startRaw, endRaw any
	if err := row.Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
		&p.IconURL, &startRaw, &endRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("now playing: %w", err)
	}
	var err error
	if p.StartTime, err = coerceSQLiteTime(startRaw); err != nil {
		return nil, fmt.Errorf("parse start_time: %w", err)
	}
	if p.EndTime, err = coerceSQLiteTime(endRaw); err != nil {
		return nil, fmt.Errorf("parse end_time: %w", err)
	}
	return p, nil
}

// Schedule returns programs for a channel within a time range.
//
// Reads via raw SQL (not sqlc) for the same reason as NowPlaying: the
// coerce helper transparently handles legacy rows whose time column
// was persisted in the Go-stringer format.
func (r *EPGProgramRepository) Schedule(ctx context.Context, channelID string, from, to time.Time) ([]*EPGProgram, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(icon_url,''), start_time, end_time
		 FROM epg_programs
		 WHERE channel_id = ? AND end_time > ? AND start_time < ?
		 ORDER BY start_time`, channelID, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("schedule: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var result []*EPGProgram
	for rows.Next() {
		p := &EPGProgram{}
		var startRaw, endRaw any
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
			&p.IconURL, &startRaw, &endRaw); err != nil {
			return nil, fmt.Errorf("scan program: %w", err)
		}
		if p.StartTime, err = coerceSQLiteTime(startRaw); err != nil {
			return nil, fmt.Errorf("parse start_time: %w", err)
		}
		if p.EndTime, err = coerceSQLiteTime(endRaw); err != nil {
			return nil, fmt.Errorf("parse end_time: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// bulkScheduleChunkSize caps how many channel IDs go into a single IN()
// clause. SQLite's default SQLITE_LIMIT_VARIABLE_NUMBER is 999 (older
// builds) or 32k (modern); 500 leaves plenty of headroom for the two
// time bounds plus whatever variants the driver binds underneath. Live
// TV libraries with thousands of channels (davidmuma, iptv-org full
// country dumps) get split into chunks transparently.
const bulkScheduleChunkSize = 500

// BulkSchedule returns programs for multiple channels within a time range.
// Uses raw SQL because sqlc doesn't support dynamic IN() on SQLite.
//
// Large channel lists are chunked internally so callers don't have to
// care about the SQLite variable limit.
func (r *EPGProgramRepository) BulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*EPGProgram, error) {
	result := make(map[string][]*EPGProgram)
	if len(channelIDs) == 0 {
		return result, nil
	}

	// Dedupe to avoid a duplicated id landing in two different chunks
	// and double-counting rows in the merged map.
	ids := dedupeStrings(channelIDs)

	for start := 0; start < len(ids); start += bulkScheduleChunkSize {
		end := start + bulkScheduleChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := r.bulkScheduleChunk(ctx, ids[start:end], from, to, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *EPGProgramRepository) bulkScheduleChunk(
	ctx context.Context,
	channelIDs []string,
	from, to time.Time,
	result map[string][]*EPGProgram,
) error {
	placeholders := "?"
	args := []any{from.UTC(), to.UTC()}
	for i, id := range channelIDs {
		if i > 0 {
			placeholders += ",?"
		}
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(icon_url,''), start_time, end_time
		 FROM epg_programs
		 WHERE end_time > ? AND start_time < ? AND channel_id IN (`+placeholders+`)
		 ORDER BY channel_id, start_time`, args...,
	)
	if err != nil {
		return fmt.Errorf("bulk schedule: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		p := &EPGProgram{}
		// start_time / end_time are scanned as `any` because rows from an
		// older build may still be persisted in the Go-stringer format
		// that modernc.org/sqlite can't deserialise directly. The coerce
		// helper handles both that legacy string form and the clean
		// time.Time the driver returns for UTC values inserted by the
		// current code.
		var startRaw, endRaw any
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
			&p.IconURL, &startRaw, &endRaw); err != nil {
			return fmt.Errorf("scan program: %w", err)
		}
		if p.StartTime, err = coerceSQLiteTime(startRaw); err != nil {
			return fmt.Errorf("parse start_time: %w", err)
		}
		if p.EndTime, err = coerceSQLiteTime(endRaw); err != nil {
			return fmt.Errorf("parse end_time: %w", err)
		}
		result[p.ChannelID] = append(result[p.ChannelID], p)
	}
	return rows.Err()
}

// sqliteTimeStringLayouts are the text encodings modernc.org/sqlite can
// emit for a TIMESTAMP column, in the order we try them. Ordered by
// how frequently each shows up in our data:
//
//   - RFC3339 with offset   — sqlc-bound UTC times round-trip this way
//   - Go default Stringer   — "2006-01-02 15:04:05 -0700 MST" produced
//     when the driver falls back to fmt.Sprint on a non-UTC time.Time.
//     Legacy rows written before the UTC-normalisation fix use this form.
//   - RFC3339 bare          — just in case
var sqliteTimeStringLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05",
}

// coerceSQLiteTime accepts whatever modernc.org/sqlite hands us for a
// TIMESTAMP column and produces a time.Time. Returns zero value for
// nil / empty strings; errors only if the value is a non-empty string
// that doesn't match any known layout.
func coerceSQLiteTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return t.UTC(), nil
	case []byte:
		return parseSQLiteTimeString(string(t))
	case string:
		return parseSQLiteTimeString(t)
	default:
		return time.Time{}, fmt.Errorf("unsupported time value type %T", v)
	}
}

func parseSQLiteTimeString(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range sqliteTimeStringLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time format: %q", s)
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// CleanupOld removes programs that ended before the given time.
func (r *EPGProgramRepository) CleanupOld(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.CleanupOldPrograms(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("cleanup old programs: %w", err)
	}
	return n, nil
}

