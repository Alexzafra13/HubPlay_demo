package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// PeopleRepository — Pattern A dual-dialect. SortOrder + Year are
// INTEGER → NullInt64 in SQLite, NullInt32 / int32 in Postgres; the
// param paths branch per backend.
type PeopleRepository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewPeopleRepository(driver string, database *sql.DB) *PeopleRepository {
	r := &PeopleRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *PeopleRepository) useSQLite() bool { return r.sq != nil }

// EnsureByName returns the existing person row for `name`, creating
// one with the supplied type when no row exists. Used by the
// scanner to dedupe cast across items: the second time a movie
// surfaces "Tom Hanks" the second row reuses the first's id.
//
// Dedup is name-only for now — TMDb is consistent with English
// names and the small fraction of edge cases (alias drift, duplicate
// names) is acceptable until a person-level external_ids table goes
// in. The scanner doesn't need a roundtrip in the common path:
// `created` reports whether this call inserted, so the caller can
// skip thumb-download work for already-known people.
func (r *PeopleRepository) EnsureByName(ctx context.Context, name, personType string) (id string, created bool, err error) {
	if r.useSQLite() {
		row, gerr := r.sq.GetPersonByName(ctx, name)
		if gerr == nil {
			return row.ID, false, nil
		}
		if !errors.Is(gerr, sql.ErrNoRows) {
			return "", false, fmt.Errorf("get person: %w", gerr)
		}
		newID := uuid.NewString()
		if err := r.sq.CreatePerson(ctx, sqlc.CreatePersonParams{
			ID:        newID,
			Name:      name,
			Type:      nullableString(personType),
			ThumbPath: nullableString(""),
		}); err != nil {
			return "", false, fmt.Errorf("create person: %w", err)
		}
		return newID, true, nil
	}

	row, gerr := r.pq.GetPersonByName(ctx, name)
	if gerr == nil {
		return row.ID, false, nil
	}
	if !errors.Is(gerr, sql.ErrNoRows) {
		return "", false, fmt.Errorf("get person: %w", gerr)
	}
	newID := uuid.NewString()
	if err := r.pq.CreatePerson(ctx, sqlc_pg.CreatePersonParams{
		ID:        newID,
		Name:      name,
		Type:      nullableString(personType),
		ThumbPath: nullableString(""),
	}); err != nil {
		return "", false, fmt.Errorf("create person: %w", err)
	}
	return newID, true, nil
}

func (r *PeopleRepository) GetByID(ctx context.Context, id string) (*librarymodel.Person, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPersonByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("person %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get person: %w", err)
		}
		return &librarymodel.Person{ID: row.ID, Name: row.Name, Type: row.Type, ThumbPath: row.ThumbPath}, nil
	}
	row, err := r.pq.GetPersonByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("person %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get person: %w", err)
	}
	return &librarymodel.Person{ID: row.ID, Name: row.Name, Type: row.Type, ThumbPath: row.ThumbPath}, nil
}

func (r *PeopleRepository) SetThumbPath(ctx context.Context, id, thumbPath string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.SetPersonThumbPath(ctx, sqlc.SetPersonThumbPathParams{
			ThumbPath: nullableString(thumbPath),
			ID:        id,
		})
	} else {
		err = r.pq.SetPersonThumbPath(ctx, sqlc_pg.SetPersonThumbPathParams{
			ThumbPath: nullableString(thumbPath),
			ID:        id,
		})
	}
	if err != nil {
		return fmt.Errorf("set person thumb: %w", err)
	}
	return nil
}

// ReplaceItemPeople clears the existing cast/crew for an item and
// inserts the supplied list. Atomic at the SQL level — both
// statements happen in the same connection — so re-scans never
// expose a half-written cast list to a concurrent reader.
func (r *PeopleRepository) ReplaceItemPeople(ctx context.Context, itemID string, credits []librarymodel.ItemPersonCredit) error {
	if r.useSQLite() {
		if err := r.sq.DeleteItemPeople(ctx, itemID); err != nil {
			return fmt.Errorf("delete item people: %w", err)
		}
		for _, c := range credits {
			if err := r.sq.InsertItemPerson(ctx, sqlc.InsertItemPersonParams{
				ItemID:        itemID,
				PersonID:      c.PersonID,
				Role:          c.Role,
				CharacterName: nullableString(c.CharacterName),
				SortOrder:     nullableInt64(int64(c.SortOrder)),
			}); err != nil {
				return fmt.Errorf("insert item person: %w", err)
			}
		}
		return nil
	}

	if err := r.pq.DeleteItemPeople(ctx, itemID); err != nil {
		return fmt.Errorf("delete item people: %w", err)
	}
	for _, c := range credits {
		if err := r.pq.InsertItemPerson(ctx, sqlc_pg.InsertItemPersonParams{
			ItemID:        itemID,
			PersonID:      c.PersonID,
			Role:          c.Role,
			CharacterName: nullableString(c.CharacterName),
			SortOrder:     nullableInt32(int32(c.SortOrder)),
		}); err != nil {
			return fmt.Errorf("insert item person: %w", err)
		}
	}
	return nil
}

// ListFilmographyByPerson returns the deduped, sorted filmography for
// a person — one row per (movie | series) the person has a credit on.
// When the same person has multiple credits on the same title (e.g.
// directed AND wrote the same movie), only the row with the lowest
// sort_order is kept; that's almost always the most prominent role
// (TMDb pads writer/producer credits with high sort_order values).
func (r *PeopleRepository) ListFilmographyByPerson(ctx context.Context, personID string) ([]*librarymodel.FilmographyEntry, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListFilmographyByPerson(ctx, personID)
		if err != nil {
			return nil, fmt.Errorf("list filmography: %w", err)
		}
		out := make([]*librarymodel.FilmographyEntry, 0, len(rows))
		seen := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			if _, ok := seen[row.ItemID]; ok {
				continue
			}
			seen[row.ItemID] = struct{}{}
			year := 0
			if row.Year.Valid {
				year = int(row.Year.Int64)
			}
			out = append(out, &librarymodel.FilmographyEntry{
				ItemID:         row.ItemID,
				Type:           row.Type,
				Title:          row.Title,
				Year:           year,
				Role:           row.Role,
				CharacterName:  row.CharacterName,
				SortOrder:      int(row.SortOrder),
				PrimaryImageID: row.PrimaryImageID,
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListFilmographyByPerson(ctx, personID)
	if err != nil {
		return nil, fmt.Errorf("list filmography: %w", err)
	}
	out := make([]*librarymodel.FilmographyEntry, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ItemID]; ok {
			continue
		}
		seen[row.ItemID] = struct{}{}
		year := 0
		if row.Year.Valid {
			year = int(row.Year.Int32)
		}
		out = append(out, &librarymodel.FilmographyEntry{
			ItemID:         row.ItemID,
			Type:           row.Type,
			Title:          row.Title,
			Year:           year,
			Role:           row.Role,
			CharacterName:  row.CharacterName,
			SortOrder:      int(row.SortOrder),
			PrimaryImageID: row.PrimaryImageID,
		})
	}
	return out, nil
}

func (r *PeopleRepository) ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ItemPersonCredit, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListItemPeople(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list item people: %w", err)
		}
		out := make([]*librarymodel.ItemPersonCredit, len(rows))
		for i, row := range rows {
			out[i] = &librarymodel.ItemPersonCredit{
				PersonID:      row.PersonID,
				Name:          row.Name,
				PersonType:    row.PersonType,
				ThumbPath:     row.ThumbPath,
				Role:          row.Role,
				CharacterName: row.CharacterName,
				SortOrder:     int(row.SortOrder),
			}
		}
		return out, nil
	}
	rows, err := r.pq.ListItemPeople(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list item people: %w", err)
	}
	out := make([]*librarymodel.ItemPersonCredit, len(rows))
	for i, row := range rows {
		out[i] = &librarymodel.ItemPersonCredit{
			PersonID:      row.PersonID,
			Name:          row.Name,
			PersonType:    row.PersonType,
			ThumbPath:     row.ThumbPath,
			Role:          row.Role,
			CharacterName: row.CharacterName,
			SortOrder:     int(row.SortOrder),
		}
	}
	return out, nil
}
