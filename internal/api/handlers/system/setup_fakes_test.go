package system

import (
	"context"
	"net/http"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
	providermodel "hubplay/internal/provider/model"
	"hubplay/internal/stream"
)

// mockAuthService is a minimal fake for handlers.AuthService.
type mockAuthService struct {
	loginFn    func(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	registerFn func(ctx context.Context, req auth.RegisterRequest) (*authmodel.User, error)
}

func (m *mockAuthService) Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, username, password, deviceName, deviceID, ip)
	}
	return &auth.AuthToken{}, nil
}
func (m *mockAuthService) RefreshToken(context.Context, string, string) (*auth.AuthToken, error) {
	return nil, nil
}
func (m *mockAuthService) Logout(context.Context, string) error { return nil }
func (m *mockAuthService) Register(ctx context.Context, req auth.RegisterRequest) (*authmodel.User, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return &authmodel.User{ID: "u1", Username: req.Username, Role: "admin"}, nil
}
func (m *mockAuthService) ResetPassword(context.Context, string) (string, error)        { return "", nil }
func (m *mockAuthService) ChangePassword(context.Context, string, string, string) error { return nil }
func (m *mockAuthService) ListProfiles(context.Context, string) ([]*authmodel.User, error) {
	return nil, nil
}
func (m *mockAuthService) SwitchProfile(context.Context, string, string, string, string, string, string) (*auth.AuthToken, error) {
	return nil, nil
}
func (m *mockAuthService) SetPIN(context.Context, string, string) error { return nil }
func (m *mockAuthService) ValidateToken(context.Context, string) (*auth.Claims, error) {
	return nil, nil
}
func (m *mockAuthService) Middleware(next http.Handler) http.Handler { return next }
func (m *mockAuthService) ListSessions(context.Context, string) ([]*authmodel.Session, error) {
	return nil, nil
}
func (m *mockAuthService) RevokeSession(context.Context, string, string) error { return nil }
func (m *mockAuthService) CurrentSessionID(context.Context, string) string     { return "" }

// libFakeService is a minimal fake for setupLibraryOps.
type libFakeService struct {
	createFn func(ctx context.Context, req library.CreateRequest) (*librarymodel.Library, error)
	listFn   func(ctx context.Context) ([]*librarymodel.Library, error)
}

func (f *libFakeService) List(ctx context.Context) ([]*librarymodel.Library, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, nil
}
func (f *libFakeService) Create(ctx context.Context, req library.CreateRequest) (*librarymodel.Library, error) {
	if f.createFn != nil {
		return f.createFn(ctx, req)
	}
	return &librarymodel.Library{ID: "lib-1", Name: req.Name}, nil
}

// userFakeService is a minimal fake for setupUserCounter.
type userFakeService struct {
	countFn func(ctx context.Context) (int, error)
}

func (f *userFakeService) Count(ctx context.Context) (int, error) {
	if f.countFn != nil {
		return f.countFn(ctx)
	}
	return 0, nil
}

// providersFakeRepo is a minimal fake for handlers.ProviderRepository.
type providersFakeRepo struct {
	getByName map[string]*providermodel.ProviderConfig
	upserted  []*providermodel.ProviderConfig
	upsertErr error
}

func (f *providersFakeRepo) ListAll(context.Context) ([]*providermodel.ProviderConfig, error) {
	return nil, nil
}
func (f *providersFakeRepo) GetByName(_ context.Context, name string) (*providermodel.ProviderConfig, error) {
	if p, ok := f.getByName[name]; ok {
		return p, nil
	}
	return nil, nil
}
func (f *providersFakeRepo) Upsert(_ context.Context, p *providermodel.ProviderConfig) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = append(f.upserted, p)
	return nil
}

// fakeStreamManager implements handlers.StreamManagerService for health tests.
type fakeStreamManager struct {
	active int
}

func newFakeStreamManager() *fakeStreamManager {
	return &fakeStreamManager{}
}

func (f *fakeStreamManager) StartSession(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
	return nil, nil
}
func (f *fakeStreamManager) GetSession(string) (*stream.ManagedSession, bool) { return nil, false }
func (f *fakeStreamManager) RestartSessionAt(string, int, float64) error      { return nil }
func (f *fakeStreamManager) StopSession(string)                               {}
func (f *fakeStreamManager) StopSessionsByItem(string, string) int            { return 0 }
func (f *fakeStreamManager) ActiveSessions() int                              { return f.active }
