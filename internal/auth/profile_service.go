package auth

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// ProfileService cubre profiles per-user (Plex-style): listing,
// switch entre profiles del mismo owner y gestión del PIN opcional.
// Comparte el `loginRateLimiter` con `LoginService` para la
// protección anti-bruteforce del PIN de 4 dígitos (10k combos —
// trivial sin rate-limit).
type ProfileService struct {
	users       *db.UserRepository
	cfg         config.AuthConfig
	clock       clock.Clock
	logger      *slog.Logger
	rateLimiter *loginRateLimiter
	issuer      *sessionIssuer
}

func newProfileService(
	users *db.UserRepository,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
	rl *loginRateLimiter,
	issuer *sessionIssuer,
) *ProfileService {
	return &ProfileService{
		users:       users,
		cfg:         cfg,
		clock:       clk,
		logger:      logger,
		rateLimiter: rl,
		issuer:      issuer,
	}
}

// ListProfiles devuelve la fila del parent + cada profile hijo. El
// caller tiene que ser parent o uno de los hijos — pasar un userID
// ajeno resuelve el owner via parent_user_id y devuelve el árbol
// correcto, pero el surface público solo se alcanza con el claim
// propio.
func (s *ProfileService) ListProfiles(ctx context.Context, userID string) ([]*authmodel.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	owner := user.ID
	if user.IsProfile() {
		owner = user.ParentUserID
	}
	return s.users.ListProfilesForOwner(ctx, owner)
}

// SwitchProfile mintea una sesión nueva para un profile hijo (o para
// el parent cuando el user quiere volver). El caller tiene que ser
// parent del target O otro hijo bajo el mismo parent — el shared
// owner-id es la puerta.
//
// PIN es opcional desde el wire; required cuando el target lleva
// pin_hash. PIN vacío contra un profile PIN-protected devuelve
// ErrInvalidPassword (compartir el mismo sentinel con clave errónea
// mantiene la surface de errores tiny — anti-enumeración).
func (s *ProfileService) SwitchProfile(
	ctx context.Context,
	currentUserID, targetProfileID, pin, deviceName, deviceID, ip string,
) (*AuthToken, error) {
	current, err := s.users.GetByID(ctx, currentUserID)
	if err != nil {
		return nil, err
	}
	target, err := s.users.GetByID(ctx, targetProfileID)
	if err != nil {
		return nil, err
	}
	// Owner es el parent (current cuando current ES el parent,
	// current.parent_user_id cuando current es un profile). Mismo
	// para target. Match → caller tiene permitido el switch.
	currentOwner := current.ID
	if current.IsProfile() {
		currentOwner = current.ParentUserID
	}
	targetOwner := target.ID
	if target.IsProfile() {
		targetOwner = target.ParentUserID
	}
	if currentOwner != targetOwner {
		return nil, fmt.Errorf("switch profile: %w", domain.ErrForbidden)
	}
	if !target.IsActive {
		return nil, fmt.Errorf("switch profile: %w", domain.ErrAccountDisabled)
	}
	// PIN gate. Hash vacío = "sin PIN, cualquiera puede switchear".
	// Hash non-empty = el caller tiene que dar un PIN que bcrypt-
	// matchee. PIN erróneo devuelve intencionadamente el mismo
	// error code que clave errónea para que un enumeration attack
	// no pueda mapearlo.
	//
	// Brute-force protection: un PIN de 4 dígitos solo tiene 10k
	// combos, así que el mismo loginRateLimiter de Login gatea este
	// path. Keyamos por target profile id (para que un flood contra
	// un profile bloquee ese profile, no la familia) Y por IP (para
	// que un atacante rotando profile ids desde la misma red trip
	// igual el lock). PIN vacío contra un profile PIN-protected
	// cuenta como failure — no es un modo normal de uso y tratarlo
	// como tal dejaría que `pin = ""` bypass el lockout.
	if target.PINHash != "" {
		pinKey := "pin:" + target.ID
		pinIPKey := "pin:ip:" + ip
		if s.rateLimiter.isLocked(pinKey) || s.rateLimiter.isLocked(pinIPKey) {
			s.logger.Warn("switch profile rate limited", "target", target.ID, "ip", ip)
			return nil, fmt.Errorf("switch profile: too many failed pin attempts: %w", domain.ErrForbidden)
		}
		if pin == "" {
			s.rateLimiter.recordFailure(pinKey)
			s.rateLimiter.recordFailure(pinIPKey)
			return nil, fmt.Errorf("switch profile: pin required: %w", domain.ErrInvalidPassword)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(target.PINHash), []byte(pin)); err != nil {
			s.rateLimiter.recordFailure(pinKey)
			s.rateLimiter.recordFailure(pinIPKey)
			return nil, fmt.Errorf("switch profile: %w", domain.ErrInvalidPassword)
		}
		s.rateLimiter.recordSuccess(pinKey)
		s.rateLimiter.recordSuccess(pinIPKey)
	}
	// El token es una sesión regular para el users.id del target —
	// cada endpoint user-keyed (user_data, favourites,
	// federation_progress, ...) sigue funcionando sin aprender qué
	// es un "profile".
	if err := s.users.UpdateLastLogin(ctx, target.ID, s.clock.Now()); err != nil {
		s.logger.Warn("switch profile: update last login", "error", err)
	}
	s.logger.Info("profile switched", "from", currentUserID, "to", targetProfileID)
	return s.issuer.issue(ctx, target, deviceName, deviceID, ip)
}

// SetPIN bcrypt-hashea (y guarda) un PIN de 4 dígitos, o lo limpia
// cuando `pin` es "". El permission check del caller pasa en el
// HTTP layer; este método confía en sus inputs por diseño (path
// admin-only).
func (s *ProfileService) SetPIN(ctx context.Context, userID, pin string) error {
	if pin == "" {
		return s.users.SetPIN(ctx, userID, "")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), s.cfg.BCryptCost)
	if err != nil {
		return fmt.Errorf("hashing pin: %w", err)
	}
	return s.users.SetPIN(ctx, userID, string(hash))
}
