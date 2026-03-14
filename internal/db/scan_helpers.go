package db

import "database/sql"

// itemNullables holds the sql.Null* intermediaries used when scanning an item row.
type itemNullables struct {
	parentID        sql.NullString
	originalTitle   sql.NullString
	path            sql.NullString
	container       sql.NullString
	fingerprint     sql.NullString
	contentRating   sql.NullString
	seasonNum       sql.NullInt32
	episodeNum      sql.NullInt32
	communityRating sql.NullFloat64
	premiereDate    sql.NullTime
}

// applyFull writes nullable values into the Item (full row with all columns).
func (n *itemNullables) applyFull(item *Item) {
	item.ParentID = n.parentID.String
	item.OriginalTitle = n.originalTitle.String
	item.Path = n.path.String
	item.Container = n.container.String
	item.Fingerprint = n.fingerprint.String
	item.ContentRating = n.contentRating.String
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
	if n.premiereDate.Valid {
		item.PremiereDate = &n.premiereDate.Time
	}
}

// applyList writes nullable values into the Item (list query without fingerprint/contentRating/premiereDate).
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

// fullScanDests returns the Scan destinations for a full item row
// (id through is_available, 21 columns).
func fullScanDests(item *Item, n *itemNullables) []any {
	return []any{
		&item.ID, &item.LibraryID, &n.parentID, &item.Type, &item.Title, &item.SortTitle,
		&n.originalTitle, &item.Year, &n.path, &item.Size, &item.DurationTicks,
		&n.container, &n.fingerprint, &n.seasonNum, &n.episodeNum, &n.communityRating,
		&n.contentRating, &n.premiereDate, &item.AddedAt, &item.UpdatedAt, &item.IsAvailable,
	}
}

// listScanDests returns the Scan destinations for the list/children query
// (without fingerprint, content_rating, premiere_date — 18 columns).
func listScanDests(item *Item, n *itemNullables) []any {
	return []any{
		&item.ID, &item.LibraryID, &n.parentID, &item.Type, &item.Title,
		&item.SortTitle, &n.originalTitle, &item.Year, &n.path, &item.Size, &item.DurationTicks,
		&n.container, &n.seasonNum, &n.episodeNum, &n.communityRating,
		&item.AddedAt, &item.UpdatedAt, &item.IsAvailable,
	}
}
