package db

import "database/sql"

// itemNullables holds the sql.Null* intermediaries used when scanning an
// item row for the list/children raw-SQL queries (the two queries that
// didn't migrate to sqlc because of FTS5 + keyset cursors — see
// docs/memory/architecture-decisions.md ADR-001).
//
// The "full" variant (with fingerprint / content_rating / premiere_date
// columns) used to live here too but those code paths are now served by
// sqlc-generated scanners; dead helpers were removed when the linter
// flagged them in CI.
type itemNullables struct {
	parentID        sql.NullString
	originalTitle   sql.NullString
	path            sql.NullString
	container       sql.NullString
	seasonNum       sql.NullInt32
	episodeNum      sql.NullInt32
	communityRating sql.NullFloat64
}

// applyList writes nullable values into the Item for list-query rows
// (18 columns — no fingerprint, content_rating or premiere_date).
func (n *itemNullables) applyList(item *Item) {
	item.ParentID = n.parentID.String
	item.OriginalTitle = n.originalTitle.String
	item.Path = n.path.String
	item.Container = n.container.String
	if n.seasonNum.Valid {
		v := int(n.seasonNum.Int32)
		item.SeasonNumber = &v
	}
	if n.episodeNum.Valid {
		v := int(n.episodeNum.Int32)
		item.EpisodeNumber = &v
	}
	if n.communityRating.Valid {
		item.CommunityRating = &n.communityRating.Float64
	}
}

// listScanDests returns the Scan destinations for the list/children query.
func listScanDests(item *Item, n *itemNullables) []any {
	return []any{
		&item.ID, &item.LibraryID, &n.parentID, &item.Type, &item.Title,
		&item.SortTitle, &n.originalTitle, &item.Year, &n.path, &item.Size, &item.DurationTicks,
		&n.container, &n.seasonNum, &n.episodeNum, &n.communityRating,
		&item.AddedAt, &item.UpdatedAt, &item.IsAvailable,
	}
}
