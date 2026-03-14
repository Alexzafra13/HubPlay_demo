package db

import (
	"context"
	"database/sql"
	"fmt"
)

type MediaStream struct {
	ItemID            string
	StreamIndex       int
	StreamType        string // video, audio, subtitle
	Codec             string
	Profile           string
	Bitrate           int
	Width             int
	Height            int
	FrameRate         float64
	HDRType           string
	ColorSpace        string
	Channels          int
	SampleRate        int
	Language          string
	Title             string
	IsDefault         bool
	IsForced          bool
	IsHearingImpaired bool
}

type MediaStreamRepository struct {
	db *sql.DB
}

func NewMediaStreamRepository(database *sql.DB) *MediaStreamRepository {
	return &MediaStreamRepository{db: database}
}

func (r *MediaStreamRepository) ReplaceForItem(ctx context.Context, itemID string, streams []*MediaStream) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM media_streams WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("delete old streams: %w", err)
	}

	for _, s := range streams {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO media_streams (item_id, stream_index, stream_type, codec, profile, bitrate,
			 width, height, frame_rate, hdr_type, color_space, channels, sample_rate,
			 language, title, is_default, is_forced, is_hearing_impaired)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ItemID, s.StreamIndex, s.StreamType, s.Codec, s.Profile, s.Bitrate,
			s.Width, s.Height, s.FrameRate, s.HDRType, s.ColorSpace,
			s.Channels, s.SampleRate, s.Language, s.Title,
			s.IsDefault, s.IsForced, s.IsHearingImpaired,
		)
		if err != nil {
			return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
		}
	}

	return tx.Commit()
}

func (r *MediaStreamRepository) ListByItem(ctx context.Context, itemID string) ([]*MediaStream, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT item_id, stream_index, stream_type, COALESCE(codec,''), COALESCE(profile,''),
		        COALESCE(bitrate,0), COALESCE(width,0), COALESCE(height,0), COALESCE(frame_rate,0),
		        COALESCE(hdr_type,''), COALESCE(color_space,''), COALESCE(channels,0),
		        COALESCE(sample_rate,0), COALESCE(language,''), COALESCE(title,''),
		        COALESCE(is_default,0), COALESCE(is_forced,0), COALESCE(is_hearing_impaired,0)
		 FROM media_streams WHERE item_id = ? ORDER BY stream_index`, itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var streams []*MediaStream
	for rows.Next() {
		s := &MediaStream{}
		if err := rows.Scan(&s.ItemID, &s.StreamIndex, &s.StreamType, &s.Codec, &s.Profile,
			&s.Bitrate, &s.Width, &s.Height, &s.FrameRate, &s.HDRType, &s.ColorSpace,
			&s.Channels, &s.SampleRate, &s.Language, &s.Title,
			&s.IsDefault, &s.IsForced, &s.IsHearingImpaired); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}
