package me

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
		if urls, ok := r.primaryURLs[id]; ok { out[id] = urls }
	}
	return out, nil
}
func (r *fakeImageRepo) ListByItem(context.Context, string) ([]*librarymodel.Image, error) { return nil, nil }
func (r *fakeImageRepo) Create(context.Context, *librarymodel.Image) error { return nil }
func (r *fakeImageRepo) SetPrimary(context.Context, string, string, string) error { return nil }
func (r *fakeImageRepo) SetLocked(context.Context, string, bool) error { return nil }
func (r *fakeImageRepo) GetByID(context.Context, string) (*librarymodel.Image, error) { return nil, nil }
func (r *fakeImageRepo) DeleteByID(context.Context, string) error { return nil }
