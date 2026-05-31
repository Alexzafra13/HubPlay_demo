package media

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
)

// ─── Fake LibraryService ────────────────────────────────────────────────────
//
// Minimal copy of the library fake (originally in library/library_test.go)
// needed by items_test.go. Only methods exercised by item tests are wired.

type libFakeService struct {
	listItemsFn     func(ctx context.Context, f librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
	getItemFn       func(ctx context.Context, id string) (*librarymodel.Item, error)
	getChildrenFn   func(ctx context.Context, id string) ([]*librarymodel.Item, error)
	getStreamsFn    func(ctx context.Context, id string) ([]*librarymodel.MediaStream, error)
	getItemImagesFn func(ctx context.Context, id string) ([]*librarymodel.Image, error)
}

func (s *libFakeService) GetItem(ctx context.Context, id string) (*librarymodel.Item, error) {
	if s.getItemFn != nil {
		return s.getItemFn(ctx, id)
	}
	return nil, domain.NewNotFound("item")
}
func (s *libFakeService) GetItemChildren(ctx context.Context, id string) ([]*librarymodel.Item, error) {
	if s.getChildrenFn != nil {
		return s.getChildrenFn(ctx, id)
	}
	return nil, nil
}
func (s *libFakeService) GetItemChildCounts(_ context.Context, _ []string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (s *libFakeService) GetItemStreams(ctx context.Context, id string) ([]*librarymodel.MediaStream, error) {
	if s.getStreamsFn != nil {
		return s.getStreamsFn(ctx, id)
	}
	return nil, nil
}
func (s *libFakeService) GetItemImages(ctx context.Context, id string) ([]*librarymodel.Image, error) {
	if s.getItemImagesFn != nil {
		return s.getItemImagesFn(ctx, id)
	}
	return nil, nil
}
func (s *libFakeService) ListItems(ctx context.Context, f librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
	if s.listItemsFn != nil {
		return s.listItemsFn(ctx, f)
	}
	return nil, 0, nil
}

// ─── Fake MetadataRepository ────────────────────────────────────────────────

type libFakeMetadataRepo struct {
	byID map[string]*librarymodel.Metadata
}

func (r *libFakeMetadataRepo) GetByItemID(_ context.Context, itemID string) (*librarymodel.Metadata, error) {
	if m, ok := r.byID[itemID]; ok {
		return m, nil
	}
	return nil, domain.NewNotFound("metadata")
}

func (r *libFakeMetadataRepo) GetMetadataBatch(_ context.Context, ids []string) (map[string]*librarymodel.Metadata, error) {
	out := map[string]*librarymodel.Metadata{}
	for _, id := range ids {
		if m, ok := r.byID[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

var _ handlers.MetadataRepository = (*libFakeMetadataRepo)(nil)

// ─── Fake UserDataRepository ────────────────────────────────────────────────

type progressFakeUserData struct {
	mu   sync.Mutex
	data map[string]*librarymodel.UserData
}

func newProgressFakeUserData() *progressFakeUserData {
	return &progressFakeUserData{data: map[string]*librarymodel.UserData{}}
}

func progressKey(userID, itemID string) string { return userID + ":" + itemID }

func (r *progressFakeUserData) Get(_ context.Context, userID, itemID string) (*librarymodel.UserData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ud, ok := r.data[progressKey(userID, itemID)]
	if !ok {
		return nil, nil
	}
	cp := *ud
	return &cp, nil
}

func (r *progressFakeUserData) GetBatch(_ context.Context, userID string, itemIDs []string) (map[string]*librarymodel.UserData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]*librarymodel.UserData, len(itemIDs))
	for _, id := range itemIDs {
		if ud, ok := r.data[progressKey(userID, id)]; ok {
			cp := *ud
			out[id] = &cp
		}
	}
	return out, nil
}

func (r *progressFakeUserData) UpdateProgress(_ context.Context, userID, itemID string, pos int64, completed bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := progressKey(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &librarymodel.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.PositionTicks = pos
	ud.Completed = completed
	now := time.Now()
	ud.LastPlayedAt = &now
	return nil
}

func (r *progressFakeUserData) MarkPlayed(_ context.Context, userID, itemID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := progressKey(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &librarymodel.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.Completed = true
	ud.PlayCount++
	return nil
}

func (r *progressFakeUserData) SetFavorite(_ context.Context, userID, itemID string, fav bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := progressKey(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &librarymodel.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.IsFavorite = fav
	return nil
}

func (r *progressFakeUserData) ContinueWatching(_ context.Context, _ string, _ int) ([]*librarymodel.ContinueWatchingItem, error) {
	return nil, nil
}

func (r *progressFakeUserData) Favorites(_ context.Context, _ string, _, _ int) ([]*librarymodel.FavoriteItem, error) {
	return nil, nil
}

func (r *progressFakeUserData) NextUp(_ context.Context, _ string, _ int) ([]*librarymodel.NextUpItem, error) {
	return nil, nil
}

func (r *progressFakeUserData) SeriesEpisodeProgress(_ context.Context, _, _ string) (int, int, error) {
	return 0, 0, nil
}

func (r *progressFakeUserData) Delete(_ context.Context, userID, itemID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, progressKey(userID, itemID))
	return nil
}

func (r *progressFakeUserData) ClearProgress(_ context.Context, userID, itemID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := progressKey(userID, itemID)
	if ud, ok := r.data[k]; ok {
		ud.PositionTicks = 0
	}
	return nil
}

var _ handlers.UserDataRepository = (*progressFakeUserData)(nil)

// ─── Quiet logger ───────────────────────────────────────────────────────────

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
