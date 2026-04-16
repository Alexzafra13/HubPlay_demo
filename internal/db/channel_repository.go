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
type Channel struct {
	ID        string
	LibraryID string
	Name      string
	Number    int
	GroupName string
	LogoURL   string
	StreamURL string
	TvgID     string
	Language  string
	Country   string
	IsActive  bool
	AddedAt   time.Time
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
