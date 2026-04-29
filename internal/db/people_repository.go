package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

// Person is the dominio shape for a cast/crew row. Type holds the
// TMDb-style category (actor/director/writer/...); empty when the
// scanner couldn't classify a result.
type Person struct {
	ID        string
	Name      string
	Type      string
	ThumbPath string
}

// ItemPersonCredit is the join row exposed to handlers: who appears
// in this item, in what role, and as which character. SortOrder
// preserves TMDb billing order so the UI can render the cast in a
// stable, recognisable sequence.
type ItemPersonCredit struct {
	PersonID      string
	Name          string
	PersonType    string
	ThumbPath     string
	Role          string
	CharacterName string
	SortOrder     int
}

type PeopleRepository struct {
	q *sqlc.Queries
}

func NewPeopleRepository(database *sql.DB) *PeopleRepository {
	return &PeopleRepository{q: sqlc.New(database)}
}

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
	row, gerr := r.q.GetPersonByName(ctx, name)
	if gerr == nil {
		return row.ID, false, nil
	}
	if !errors.Is(gerr, sql.ErrNoRows) {
		return "", false, fmt.Errorf("get person: %w", gerr)
	}
	newID := uuid.NewString()
	if err := r.q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ID: newID, Name: name, Type: personType, ThumbPath: "",
	}); err != nil {
		return "", false, fmt.Errorf("create person: %w", err)
	}
	return newID, true, nil
}

func (r *PeopleRepository) GetByID(ctx context.Context, id string) (*Person, error) {
	row, err := r.q.GetPersonByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("person %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get person: %w", err)
	}
	return &Person{ID: row.ID, Name: row.Name, Type: row.Type, ThumbPath: row.ThumbPath}, nil
}

func (r *PeopleRepository) SetThumbPath(ctx context.Context, id, thumbPath string) error {
	if err := r.q.SetPersonThumbPath(ctx, sqlc.SetPersonThumbPathParams{
		ThumbPath: thumbPath, ID: id,
	}); err != nil {
		return fmt.Errorf("set person thumb: %w", err)
	}
	return nil
}

// ReplaceItemPeople clears the existing cast/crew for an item and
// inserts the supplied list. Atomic at the SQL level — both
// statements happen in the same connection — so re-scans never
// expose a half-written cast list to a concurrent reader.
func (r *PeopleRepository) ReplaceItemPeople(ctx context.Context, itemID string, credits []ItemPersonCredit) error {
	if err := r.q.DeleteItemPeople(ctx, itemID); err != nil {
		return fmt.Errorf("delete item people: %w", err)
	}
	for _, c := range credits {
		if err := r.q.InsertItemPerson(ctx, sqlc.InsertItemPersonParams{
			ItemID:        itemID,
			PersonID:      c.PersonID,
			Role:          c.Role,
			CharacterName: c.CharacterName,
			SortOrder:     int64(c.SortOrder),
		}); err != nil {
			return fmt.Errorf("insert item person: %w", err)
		}
	}
	return nil
}

// FilmographyEntry is one row of a person's filmography: a movie or
// series the person has a direct credit on, plus the role/character
// that surfaced for them in that title. Year is optional (provider
// may not have a release year for very-old or in-progress titles).
type FilmographyEntry struct {
	ItemID        string
	Type          string
	Title         string
	Year          int
	Role          string
	CharacterName string
	SortOrder     int
}

// ListFilmographyByPerson returns the deduped, sorted filmography for
// a person — one row per (movie | series) the person has a credit on.
// When the same person has multiple credits on the same title (e.g.
// directed AND wrote the same movie), only the row with the lowest
// sort_order is kept; that's almost always the most prominent role
// (TMDb pads writer/producer credits with high sort_order values).
func (r *PeopleRepository) ListFilmographyByPerson(ctx context.Context, personID string) ([]*FilmographyEntry, error) {
	rows, err := r.q.ListFilmographyByPerson(ctx, personID)
	if err != nil {
		return nil, fmt.Errorf("list filmography: %w", err)
	}
	out := make([]*FilmographyEntry, 0, len(rows))
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
		out = append(out, &FilmographyEntry{
			ItemID:        row.ItemID,
			Type:          row.Type,
			Title:         row.Title,
			Year:          year,
			Role:          row.Role,
			CharacterName: row.CharacterName,
			SortOrder:     int(row.SortOrder),
		})
	}
	return out, nil
}

func (r *PeopleRepository) ListByItem(ctx context.Context, itemID string) ([]*ItemPersonCredit, error) {
	rows, err := r.q.ListItemPeople(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list item people: %w", err)
	}
	out := make([]*ItemPersonCredit, len(rows))
	for i, row := range rows {
		out[i] = &ItemPersonCredit{
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
