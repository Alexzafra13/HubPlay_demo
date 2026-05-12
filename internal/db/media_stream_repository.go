package db

import (
	"context"
	"database/sql"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
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

// MediaStreamRepository — Pattern A dual-dialect. 2 methods +
// transaction in ReplaceForItem.
type MediaStreamRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewMediaStreamRepository(driver string, database *sql.DB) *MediaStreamRepository {
	r := &MediaStreamRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *MediaStreamRepository) useSQLite() bool { return r.sq != nil }

// ReplaceForItem deletes all existing streams for the item and
// inserts the new set inside a single transaction.
func (r *MediaStreamRepository) ReplaceForItem(ctx context.Context, itemID string, streams []*MediaStream) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteMediaStreamsByItem(ctx, itemID); err != nil {
			return fmt.Errorf("delete old streams: %w", err)
		}
		for _, s := range streams {
			if err := qtx.InsertMediaStream(ctx, mediaStreamToSqliteInsertParams(s)); err != nil {
				return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
			}
		}
		return tx.Commit()
	}

	qtx := r.pq.WithTx(tx)
	if err := qtx.DeleteMediaStreamsByItem(ctx, itemID); err != nil {
		return fmt.Errorf("delete old streams: %w", err)
	}
	for _, s := range streams {
		if err := qtx.InsertMediaStream(ctx, mediaStreamToPgInsertParams(s)); err != nil {
			return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
		}
	}
	return tx.Commit()
}

func (r *MediaStreamRepository) ListByItem(ctx context.Context, itemID string) ([]*MediaStream, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListMediaStreamsByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list streams: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		out := make([]*MediaStream, len(rows))
		for i, row := range rows {
			s := mediaStreamFromSqliteRow(row)
			out[i] = &s
		}
		return out, nil
	}
	rows, err := r.pq.ListMediaStreamsByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*MediaStream, len(rows))
	for i, row := range rows {
		s := mediaStreamFromPgRow(row)
		out[i] = &s
	}
	return out, nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func mediaStreamToSqliteInsertParams(s *MediaStream) sqlc.InsertMediaStreamParams {
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

func mediaStreamToPgInsertParams(s *MediaStream) sqlc_pg.InsertMediaStreamParams {
	return sqlc_pg.InsertMediaStreamParams{
		ItemID:            s.ItemID,
		StreamIndex:       int32(s.StreamIndex),
		StreamType:        s.StreamType,
		Codec:             nullableString(s.Codec),
		Profile:           nullableString(s.Profile),
		Bitrate:           nullableInt32(int32(s.Bitrate)),
		Width:             nullableInt32(int32(s.Width)),
		Height:            nullableInt32(int32(s.Height)),
		FrameRate:         nullableFloat64(s.FrameRate),
		HdrType:           nullableString(s.HDRType),
		ColorSpace:        nullableString(s.ColorSpace),
		Channels:          nullableInt32(int32(s.Channels)),
		SampleRate:        nullableInt32(int32(s.SampleRate)),
		Language:          nullableString(s.Language),
		Title:             nullableString(s.Title),
		IsDefault:         sql.NullBool{Bool: s.IsDefault, Valid: true},
		IsForced:          sql.NullBool{Bool: s.IsForced, Valid: true},
		IsHearingImpaired: sql.NullBool{Bool: s.IsHearingImpaired, Valid: true},
	}
}

func mediaStreamFromSqliteRow(r sqlc.ListMediaStreamsByItemRow) MediaStream {
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

func mediaStreamFromPgRow(r sqlc_pg.ListMediaStreamsByItemRow) MediaStream {
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

func nullableInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

// nullableInt32 mirrors nullableInt64 for the int32 type sqlc emits
// on the Postgres side for non-BIGINT integer columns.
func nullableInt32(v int32) sql.NullInt32 {
	if v == 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: v, Valid: true}
}

func nullableFloat64(v float64) sql.NullFloat64 {
	if v == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}
