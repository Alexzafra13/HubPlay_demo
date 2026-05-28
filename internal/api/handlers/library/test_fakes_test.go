package libhandler

import (
	"context"

	librarymodel "hubplay/internal/library/model"
)

type fakeImageRepo struct {
	primaryURLs map[string]map[string]librarymodel.PrimaryImageRef
}

func newFakeImageRepo() *fakeImageRepo {
	return &fakeImageRepo{}
}

func (r *fakeImageRepo) GetPrimaryURLs(_ context.Context, ids []string) (map[string]map[string]librarymodel.PrimaryImageRef, error) {
	out := make(map[string]map[string]librarymodel.PrimaryImageRef, len(ids))
	for _, id := range ids {
		if urls, ok := r.primaryURLs[id]; ok {
			out[id] = urls
		}
	}
	return out, nil
}
func (r *fakeImageRepo) ListByItem(context.Context, string) ([]*librarymodel.Image, error) { return nil, nil }
func (r *fakeImageRepo) Create(context.Context, *librarymodel.Image) error { return nil }
func (r *fakeImageRepo) SetPrimary(context.Context, string, string, string) error { return nil }
func (r *fakeImageRepo) SetLocked(context.Context, string, bool) error { return nil }
func (r *fakeImageRepo) GetByID(context.Context, string) (*librarymodel.Image, error) { return nil, nil }
func (r *fakeImageRepo) DeleteByID(context.Context, string) error { return nil }

type progressFakeUserData struct {
	data map[string]*librarymodel.UserData // key: "userID:itemID"
}

func newProgressFakeUserData() *progressFakeUserData {
	return &progressFakeUserData{data: map[string]*librarymodel.UserData{}}
}

func progressKey(userID, itemID string) string { return userID + ":" + itemID }

func (f *progressFakeUserData) Get(_ context.Context, userID, itemID string) (*librarymodel.UserData, error) {
	if ud, ok := f.data[progressKey(userID, itemID)]; ok {
		return ud, nil
	}
	return nil, nil
}
func (f *progressFakeUserData) GetBatch(_ context.Context, userID string, itemIDs []string) (map[string]*librarymodel.UserData, error) {
	out := map[string]*librarymodel.UserData{}
	for _, id := range itemIDs {
		if ud, ok := f.data[progressKey(userID, id)]; ok {
			out[id] = ud
		}
	}
	return out, nil
}
func (f *progressFakeUserData) UpdateProgress(context.Context, string, string, int64, bool) error { return nil }
func (f *progressFakeUserData) MarkPlayed(context.Context, string, string) error { return nil }
func (f *progressFakeUserData) SetFavorite(context.Context, string, string, bool) error { return nil }
func (f *progressFakeUserData) ContinueWatching(context.Context, string, int) ([]*librarymodel.ContinueWatchingItem, error) { return nil, nil }
func (f *progressFakeUserData) Favorites(context.Context, string, int, int) ([]*librarymodel.FavoriteItem, error) { return nil, nil }
func (f *progressFakeUserData) NextUp(context.Context, string, int) ([]*librarymodel.NextUpItem, error) { return nil, nil }
func (f *progressFakeUserData) SeriesEpisodeProgress(context.Context, string, string) (int, int, error) { return 0, 0, nil }
func (f *progressFakeUserData) Delete(context.Context, string, string) error { return nil }
func (f *progressFakeUserData) ClearProgress(context.Context, string, string) error { return nil }
