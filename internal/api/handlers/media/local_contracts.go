package media

import (
	"context"
	"net/url"

	authmodel "hubplay/internal/auth/model"
)

type userProfileLookup interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}

type settingsReader interface {
	GetOr(ctx context.Context, key, def string) (string, error)
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func extensionForContentType(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}
