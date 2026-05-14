package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/federation"
)

// UpsertProgress writes or replaces the user's playback state for a
// (peer, remote_item) pair. Duration is preserved across updates that
// pass 0 -- see the SQL upsert for the rationale.
func (r *Repository) UpsertProgress(ctx context.Context, p *federation.Progress) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertFederationProgress(ctx, sqlc.UpsertFederationProgressParams{
			UserID:        p.UserID,
			PeerID:        p.PeerID,
			RemoteItemID:  p.RemoteItemID,
			PositionTicks: p.PositionTicks,
			DurationTicks: p.DurationTicks,
			Completed:     p.Completed,
			LastPlayedAt:  p.LastPlayedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	} else {
		err = r.pq.UpsertFederationProgress(ctx, sqlc_pg.UpsertFederationProgressParams{
			UserID:        p.UserID,
			PeerID:        p.PeerID,
			RemoteItemID:  p.RemoteItemID,
			PositionTicks: p.PositionTicks,
			DurationTicks: p.DurationTicks,
			Completed:     p.Completed,
			LastPlayedAt:  p.LastPlayedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert federation progress: %w", err)
	}
	return nil
}

// GetProgress returns nil, nil when there's no row -- the player
// treats that as "start from 0" with no special-casing.
func (r *Repository) GetProgress(ctx context.Context, userID, peerID, remoteItemID string) (*federation.Progress, error) {
	if r.useSQLite() {
		row, err := r.sq.GetFederationProgress(ctx, sqlc.GetFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, fmt.Errorf("get federation progress: %w", err)
		}
		return &federation.Progress{
			UserID:        row.UserID,
			PeerID:        row.PeerID,
			RemoteItemID:  row.RemoteItemID,
			PositionTicks: row.PositionTicks,
			DurationTicks: row.DurationTicks,
			Completed:     row.Completed,
			LastPlayedAt:  row.LastPlayedAt,
			UpdatedAt:     row.UpdatedAt,
		}, nil
	}
	row, err := r.pq.GetFederationProgress(ctx, sqlc_pg.GetFederationProgressParams{
		UserID:       userID,
		PeerID:       peerID,
		RemoteItemID: remoteItemID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get federation progress: %w", err)
	}
	return &federation.Progress{
		UserID:        row.UserID,
		PeerID:        row.PeerID,
		RemoteItemID:  row.RemoteItemID,
		PositionTicks: row.PositionTicks,
		DurationTicks: row.DurationTicks,
		Completed:     row.Completed,
		LastPlayedAt:  row.LastPlayedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func (r *Repository) DeleteProgress(ctx context.Context, userID, peerID, remoteItemID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteFederationProgress(ctx, sqlc.DeleteFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
	} else {
		err = r.pq.DeleteFederationProgress(ctx, sqlc_pg.DeleteFederationProgressParams{
			UserID:       userID,
			PeerID:       peerID,
			RemoteItemID: remoteItemID,
		})
	}
	if err != nil {
		return fmt.Errorf("delete federation progress: %w", err)
	}
	return nil
}

// ListContinueWatching returns the user's in-progress federated items
// ordered by last_played_at desc, joined with the catalog cache for
// title / poster availability. Rows whose cache entry has been evicted
// are dropped silently -- the rail prefers no row over a title-less
// row.
func (r *Repository) ListContinueWatching(ctx context.Context, userID string, limit int) ([]*federation.PeerContinueWatchingItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if r.useSQLite() {
		rows, err := r.sq.ListFederationContinueWatching(ctx, sqlc.ListFederationContinueWatchingParams{
			UserID: userID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list federation continue watching: %w", err)
		}
		out := make([]*federation.PeerContinueWatchingItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.PeerContinueWatchingItem{
				PeerID:        row.PeerID,
				PeerName:      row.PeerName,
				LibraryID:     row.LibraryID,
				RemoteItemID:  row.RemoteItemID,
				Type:          row.Type,
				Title:         row.Title,
				Year:          int(row.Year),
				Overview:      row.Overview,
				HasPoster:     row.HasPoster,
				PositionTicks: row.PositionTicks,
				DurationTicks: row.DurationTicks,
				LastPlayedAt:  row.LastPlayedAt,
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListFederationContinueWatching(ctx, sqlc_pg.ListFederationContinueWatchingParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list federation continue watching: %w", err)
	}
	out := make([]*federation.PeerContinueWatchingItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, &federation.PeerContinueWatchingItem{
			PeerID:        row.PeerID,
			PeerName:      row.PeerName,
			LibraryID:     row.LibraryID,
			RemoteItemID:  row.RemoteItemID,
			Type:          row.Type,
			Title:         row.Title,
			Year:          int(row.Year),
			Overview:      row.Overview,
			HasPoster:     row.HasPoster,
			PositionTicks: row.PositionTicks,
			DurationTicks: row.DurationTicks,
			LastPlayedAt:  row.LastPlayedAt,
		})
	}
	return out, nil
}
