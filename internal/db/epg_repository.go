package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
	db *sql.DB
}

func NewEPGProgramRepository(database *sql.DB) *EPGProgramRepository {
	return &EPGProgramRepository{db: database}
}

// ReplaceForChannel deletes all programs for a channel and inserts new ones.
func (r *EPGProgramRepository) ReplaceForChannel(ctx context.Context, channelID string, programs []*EPGProgram) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM epg_programs WHERE channel_id = ?`, channelID); err != nil {
		return fmt.Errorf("delete old programs: %w", err)
	}

	for _, p := range programs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO epg_programs (id, channel_id, title, description, category, icon_url, start_time, end_time)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.ChannelID, p.Title, p.Description, p.Category, p.IconURL, p.StartTime, p.EndTime,
		)
		if err != nil {
			return fmt.Errorf("insert program: %w", err)
		}
	}

	return tx.Commit()
}

// NowPlaying returns the currently airing program for a channel.
func (r *EPGProgramRepository) NowPlaying(ctx context.Context, channelID string) (*EPGProgram, error) {
	p := &EPGProgram{}
	now := time.Now()
	err := r.db.QueryRowContext(ctx,
		`SELECT id, channel_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(icon_url,''), start_time, end_time
		 FROM epg_programs
		 WHERE channel_id = ? AND start_time <= ? AND end_time > ?
		 LIMIT 1`, channelID, now, now,
	).Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
		&p.IconURL, &p.StartTime, &p.EndTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("now playing: %w", err)
	}
	return p, nil
}

// Schedule returns programs for a channel within a time range.
func (r *EPGProgramRepository) Schedule(ctx context.Context, channelID string, from, to time.Time) ([]*EPGProgram, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(icon_url,''), start_time, end_time
		 FROM epg_programs
		 WHERE channel_id = ? AND end_time > ? AND start_time < ?
		 ORDER BY start_time`, channelID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("schedule: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var programs []*EPGProgram
	for rows.Next() {
		p := &EPGProgram{}
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.Title, &p.Description, &p.Category,
			&p.IconURL, &p.StartTime, &p.EndTime); err != nil {
			return nil, fmt.Errorf("scan program: %w", err)
		}
		programs = append(programs, p)
	}
	return programs, rows.Err()
}

// BulkSchedule returns programs for multiple channels within a time range.
func (r *EPGProgramRepository) BulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*EPGProgram, error) {
	if len(channelIDs) == 0 {
		return make(map[string][]*EPGProgram), nil
	}

	// Build placeholders
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
	res, err := r.db.ExecContext(ctx, `DELETE FROM epg_programs WHERE end_time < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("cleanup old programs: %w", err)
	}
	return res.RowsAffected()
}
