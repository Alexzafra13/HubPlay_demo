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
			StartTime:   p.StartTime,
			EndTime:     p.EndTime,
		})
		if err != nil {
			return fmt.Errorf("insert program: %w", err)
		}
	}

	return tx.Commit()
}

// NowPlaying returns the currently airing program for a channel.
func (r *EPGProgramRepository) NowPlaying(ctx context.Context, channelID string) (*EPGProgram, error) {
	now := time.Now()
	row, err := r.q.GetNowPlaying(ctx, sqlc.GetNowPlayingParams{
		ChannelID: channelID,
		StartTime: now,
		EndTime:   now,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("now playing: %w", err)
	}
	p := epgFromGetRow(row)
	return &p, nil
}

// Schedule returns programs for a channel within a time range.
func (r *EPGProgramRepository) Schedule(ctx context.Context, channelID string, from, to time.Time) ([]*EPGProgram, error) {
	rows, err := r.q.ListSchedule(ctx, sqlc.ListScheduleParams{
		ChannelID: channelID,
		EndTime:   from,
		StartTime: to,
	})
	if err != nil {
		return nil, fmt.Errorf("schedule: %w", err)
	}
	return epgsFromScheduleRows(rows), nil
}

// BulkSchedule returns programs for multiple channels within a time range.
// Uses raw SQL because sqlc doesn't support dynamic IN() on SQLite.
func (r *EPGProgramRepository) BulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*EPGProgram, error) {
	if len(channelIDs) == 0 {
		return make(map[string][]*EPGProgram), nil
	}

	placeholders := "?"
	args := []any{from, to}
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
		return nil, fmt.Errorf("bulk schedule: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string][]*EPGProgram)
	for rows.Next() {
		p := &EPGProgram{}
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
			&p.IconURL, &p.StartTime, &p.EndTime); err != nil {
			return nil, fmt.Errorf("scan program: %w", err)
		}
		result[p.ChannelID] = append(result[p.ChannelID], p)
	}
	return result, rows.Err()
}

// CleanupOld removes programs that ended before the given time.
func (r *EPGProgramRepository) CleanupOld(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.CleanupOldPrograms(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("cleanup old programs: %w", err)
	}
	return n, nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func epgFromGetRow(r sqlc.GetNowPlayingRow) EPGProgram {
	return EPGProgram{
		ID:          r.ID,
		ChannelID:   r.ChannelID,
		Title:       r.Title,
		Description: r.Description,
		Category:    r.Category,
		IconURL:     r.IconUrl,
		StartTime:   r.StartTime,
		EndTime:     r.EndTime,
	}
}

func epgFromScheduleRow(r sqlc.ListScheduleRow) EPGProgram {
	return EPGProgram{
		ID:          r.ID,
		ChannelID:   r.ChannelID,
		Title:       r.Title,
		Description: r.Description,
		Category:    r.Category,
		IconURL:     r.IconUrl,
		StartTime:   r.StartTime,
		EndTime:     r.EndTime,
	}
}

func epgsFromScheduleRows(rows []sqlc.ListScheduleRow) []*EPGProgram {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*EPGProgram, len(rows))
	for i, row := range rows {
		p := epgFromScheduleRow(row)
		out[i] = &p
	}
	return out
}
