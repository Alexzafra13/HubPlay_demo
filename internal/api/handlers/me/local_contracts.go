package me

import (
	"context"

	authmodel "hubplay/internal/auth/model"
)

type userProfileLookup interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}
