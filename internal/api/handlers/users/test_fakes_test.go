package users

import (
	"context"

	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
)

type libFakeService struct {
	getByIDFn              func(ctx context.Context, id string) (*librarymodel.Library, error)
	createIPTVFn           func(ctx context.Context, ownerUserID string, req library.CreateRequest) (*librarymodel.Library, error)
	createPersonalIPTVFn   func(ctx context.Context, ownerUserID string, req library.CreateRequest) (*librarymodel.Library, error)
	listAccessFn           func(ctx context.Context, userID string) ([]string, error)
	replaceAccessFn        func(ctx context.Context, userID string, libraryIDs []string) error
	replaceAccessCalls     []struct{ UserID string; LibraryIDs []string }
	createPersonalIPTVCalls []struct{ OwnerUserID string; Req library.CreateRequest }
}

func (f *libFakeService) GetByID(ctx context.Context, id string) (*librarymodel.Library, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, id)
	}
	return &librarymodel.Library{ID: id, Name: "lib-" + id}, nil
}

func (f *libFakeService) CreatePersonalIPTV(ctx context.Context, ownerUserID string, req library.CreateRequest) (*librarymodel.Library, error) {
	f.createPersonalIPTVCalls = append(f.createPersonalIPTVCalls, struct{ OwnerUserID string; Req library.CreateRequest }{ownerUserID, req})
	if f.createPersonalIPTVFn != nil {
		return f.createPersonalIPTVFn(ctx, ownerUserID, req)
	}
	if f.createIPTVFn != nil {
		return f.createIPTVFn(ctx, ownerUserID, req)
	}
	return &librarymodel.Library{ID: "lib-iptv", Name: req.Name}, nil
}

func (f *libFakeService) ListAccessByUser(ctx context.Context, userID string) ([]string, error) {
	if f.listAccessFn != nil {
		return f.listAccessFn(ctx, userID)
	}
	return nil, nil
}

func (f *libFakeService) ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error {
	f.replaceAccessCalls = append(f.replaceAccessCalls, struct{ UserID string; LibraryIDs []string }{userID, libraryIDs})
	if f.replaceAccessFn != nil {
		return f.replaceAccessFn(ctx, userID, libraryIDs)
	}
	return nil
}
