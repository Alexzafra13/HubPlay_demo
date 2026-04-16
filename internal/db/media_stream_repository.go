package db

import (
	"context"
	"database/sql"
	"fmt"

	"hubplay/internal/db/sqlc"
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
	q  *sqlc.Queries
}

func NewMediaStreamRepository(database *sql.DB) *MediaStreamRepository {
	return &MediaStreamRepository{db: database, q: sqlc.New(database)}
}

// ReplaceForItem deletes all existing streams for the item and inserts the new
// set inside a single transaction.
func (r *MediaStreamRepository) ReplaceForItem(ctx context.Context, itemID string, streams []*MediaStream) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteMediaStreamsByItem(ctx, itemID); err != nil {
		return fmt.Errorf("delete old streams: %w", err)
	}

	for _, s := range streams {
		err := qtx.InsertMediaStream(ctx, mediaStreamToInsertParams(s))
		if err != nil {
			return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
		}
	}

	return tx.Commit()
}

func (r *MediaStreamRepository) ListByItem(ctx context.Context, itemID string) ([]*MediaStream, error) {
	rows, err := r.q.ListMediaStreamsByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	return mediaStreamsFromRows(rows), nil
}

func mediaStreamToInsertParams(s *MediaStream) sqlc.InsertMediaStreamParams {
	return sqlc.InsertMediaStreamParams{
		ItemID:            s.ItemID,
		StreamIndex:       int64(s.StreamIndex),
		StreamType:        s.StreamType,
		Codec:             nullableString(s.Codec),
		Profile:           nullableString(s.Profile),
		Bitrate:           nullableInt64(int64(s.Bitrate)),
		Width:             nullableInt64(int64(s.Width)),
		Height:            nullableInt64(int64(s.Height)),
		FrameRate:         nullableFloat64(s.FrameRate),
		HdrType:           nullableString(s.HDRType),
		ColorSpace:        nullableString(s.ColorSpace),
		Channels:          nullableInt64(int64(s.Channels)),
		SampleRate:        nullableInt64(int64(s.SampleRate)),
		Language:          nullableString(s.Language),
		Title:             nullableString(s.Title),
		IsDefault:         sql.NullBool{Bool: s.IsDefault, Valid: true},
		IsForced:          sql.NullBool{Bool: s.IsForced, Valid: true},
		IsHearingImpaired: sql.NullBool{Bool: s.IsHearingImpaired, Valid: true},
	}
}

func mediaStreamFromRow(r sqlc.ListMediaStreamsByItemRow) MediaStream {
	return MediaStream{
		ItemID:            r.ItemID,
		StreamIndex:       int(r.StreamIndex),
		StreamType:        r.StreamType,
		Codec:             r.Codec,
		Profile:           r.Profile,
		Bitrate:           int(r.Bitrate),
		Width:             int(r.Width),
		Height:            int(r.Height),
		FrameRate:         r.FrameRate,
		HDRType:           r.HdrType,
		ColorSpace:        r.ColorSpace,
		Channels:          int(r.Channels),
		SampleRate:        int(r.SampleRate),
		Language:          r.Language,
		Title:             r.Title,
		IsDefault:         r.IsDefault,
		IsForced:          r.IsForced,
		IsHearingImpaired: r.IsHearingImpaired,
	}
}

func mediaStreamsFromRows(rows []sqlc.ListMediaStreamsByItemRow) []*MediaStream {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*MediaStream, len(rows))
	for i, row := range rows {
		s := mediaStreamFromRow(row)
		out[i] = &s
	}
	return out
}

func nullableInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullableFloat64(v float64) sql.NullFloat64 {
	if v == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}
