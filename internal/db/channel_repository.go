package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
}

func NewChannelRepository(database *sql.DB) *ChannelRepository {
	return &ChannelRepository{db: database}
}

// Create inserts a new channel.
func (r *ChannelRepository) Create(ctx context.Context, ch *Channel) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels (id, library_id, name, number, group_name, logo_url,
		 stream_url, tvg_id, language, country, is_active, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.LibraryID, ch.Name, ch.Number, ch.GroupName, ch.LogoURL,
		ch.StreamURL, ch.TvgID, ch.Language, ch.Country, ch.IsActive, ch.AddedAt,
	)
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

// GetByID returns a channel by ID.
func (r *ChannelRepository) GetByID(ctx context.Context, id string) (*Channel, error) {
	ch := &Channel{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, name, number, COALESCE(group_name,''),
		        COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
		        COALESCE(language,''), COALESCE(country,''), is_active, added_at
		 FROM channels WHERE id = ?`, id,
	).Scan(&ch.ID, &ch.LibraryID, &ch.Name, &ch.Number, &ch.GroupName,
		&ch.LogoURL, &ch.StreamURL, &ch.TvgID, &ch.Language, &ch.Country,
		&ch.IsActive, &ch.AddedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel %s: %w", id, ErrChannelNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return ch, nil
}

// ErrChannelNotFound is returned when a channel doesn't exist.
var ErrChannelNotFound = fmt.Errorf("channel not found")

// ListByLibrary returns all channels in a library.
func (r *ChannelRepository) ListByLibrary(ctx context.Context, libraryID string, activeOnly bool) ([]*Channel, error) {
	query := `SELECT id, library_id, name, number, COALESCE(group_name,''),
	                  COALESCE(logo_url,''), stream_url, COALESCE(tvg_id,''),
	                  COALESCE(language,''), COALESCE(country,''), is_active, added_at
	           FROM channels WHERE library_id = ?`
	if activeOnly {
		query += ` AND is_active = 1`
	}
	query += ` ORDER BY number, name`

	rows, err := r.db.QueryContext(ctx, query, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var channels []*Channel
	for rows.Next() {
		ch := &Channel{}
		if err := rows.Scan(&ch.ID, &ch.LibraryID, &ch.Name, &ch.Number, &ch.GroupName,
			&ch.LogoURL, &ch.StreamURL, &ch.TvgID, &ch.Language, &ch.Country,
			&ch.IsActive, &ch.AddedAt); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// ReplaceForLibrary deletes all channels in a library and inserts new ones (used during M3U refresh).
func (r *ChannelRepository) ReplaceForLibrary(ctx context.Context, libraryID string, channels []*Channel) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM channels WHERE library_id = ?`, libraryID); err != nil {
		return fmt.Errorf("delete old channels: %w", err)
	}

	for _, ch := range channels {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO channels (id, library_id, name, number, group_name, logo_url,
			 stream_url, tvg_id, language, country, is_active, added_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ch.ID, ch.LibraryID, ch.Name, ch.Number, ch.GroupName, ch.LogoURL,
			ch.StreamURL, ch.TvgID, ch.Language, ch.Country, ch.IsActive, ch.AddedAt,
		)
		if err != nil {
			return fmt.Errorf("insert channel %s: %w", ch.Name, err)
		}
	}

	return tx.Commit()
}

// SetActive enables or disables a channel.
func (r *ChannelRepository) SetActive(ctx context.Context, id string, active bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE channels SET is_active = ? WHERE id = ?`, active, id)
	if err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// Groups returns distinct group names for a library.
func (r *ChannelRepository) Groups(ctx context.Context, libraryID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT group_name FROM channels
		 WHERE library_id = ? AND group_name != ''
		 ORDER BY group_name`, libraryID,
	)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
