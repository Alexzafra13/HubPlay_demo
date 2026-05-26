package auth

import (
	"context"
	"log/slog"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
)

// AuthToken es el payload del par access/refresh + metadata mínima.
// Devuelto por Login, RefreshToken y SwitchProfile.
type AuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id"`
	Role         string    `json:"role"`
}

// RegisterRequest es el input de AccountService.Register. Vive en
// service.go porque es la API pública del paquete que consumen
// handlers (Setup wizard + admin user creation).
type RegisterRequest struct {
	Username    string
	DisplayName string
	Password    string
	Role        string
	// PasswordChangeRequired fuerza que el próximo login exitoso
	// aterrice en la pantalla de cambio de clave. Set a true para
	// creación admin-driven cuando la clave es server-generated.
	PasswordChangeRequired bool
	// ParentUserID, cuando viene set, hace que la nueva fila sea un
	// profile bajo `ParentUserID` en lugar de una cuenta standalone.
	// Profiles comparten clave del parent y se autentican via
	// switch-profile en lugar del login handshake normal.
	ParentUserID string
}

// Service es el facade del paquete auth — embed de los 4 sub-services
// que cubren responsabilidades disjuntas (cierra el olor QQ del audit
// 2026-05-14: god-service con 18 métodos y 6 responsabilidades).
//
// Estructura:
//
//	LoginService    → Login, RefreshToken, ValidateToken, rate-limit
//	AccountService  → Register, ResetPassword, ChangePassword
//	SessionService  → ListSessions, RevokeSession, CurrentSessionID,
//	                  Logout, InvalidateUserSessions, session-cleaner
//	ProfileService  → ListProfiles, SwitchProfile, SetPIN (PIN-gated)
//
// Los handlers HTTP siguen consumiendo `auth.Service` como antes —
// la promoción de métodos vía embedding mantiene el surface
// retrocompatible. Tests `s.Login(...)` siguen llamando al método
// promovido de `*LoginService` sin cambios.
//
// Estado compartido:
//
//	users        — los 4 sub-services lo necesitan
//	rateLimiter  — Login + ProfileService (PIN brute-force gate)
//	publisher    — Login + Session (eventos UserLoggedIn / Out);
//	               mutable via SetEventBus (un mismo *publisher
//	               compartido por ambos así un solo Set actualiza
//	               todos los publishers a la vez)
//	issuer       — Login + Profile (ambos minean sesiones nuevas)
type Service struct {
	*LoginService
	*AccountService
	*SessionService
	*ProfileService

	keys        *KeyStore
	rateLimiter *loginRateLimiter
	pub         *publisher
	// clock e issuer se duplican como campos top-level del facade
	// para que callers del mismo paquete (device.go, tests) que
	// accedan `s.clock` o `s.issueSession(...)` no choquen con la
	// ambigüedad del embedding (los 4 sub-services tienen su propio
	// `clock`; Go shadowing del outer struct sobre el embedded
	// resuelve la ambigüedad).
	clock  clock.Clock
	issuer *sessionIssuer
}

// NewService construye el facade + los 4 sub-services. La duplicación
// de defaults del rate-limit se resuelve aquí (los sub-services
// reciben el limiter ya configurado).
func NewService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	keys *KeyStore,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
	rlCfg ...config.RateLimitConfig,
) *Service {
	authLogger := logger.With("module", "auth")

	maxFails := 10
	window := 15 * time.Minute
	lockout := 5 * time.Minute
	if len(rlCfg) > 0 {
		rl := rlCfg[0]
		if rl.LoginAttempts > 0 {
			maxFails = rl.LoginAttempts
		}
		if rl.LoginWindow > 0 {
			window = rl.LoginWindow
		}
		if rl.LoginLockout > 0 {
			lockout = rl.LoginLockout
		}
	}

	rl := newLoginRateLimiter(maxFails, window, lockout, clk)
	pub := &publisher{}
	issuer := newSessionIssuer(users, sessions, keys, cfg, clk, authLogger)

	return &Service{
		LoginService:   newLoginService(users, sessions, keys, cfg, clk, authLogger, rl, issuer, pub),
		AccountService: newAccountService(users, sessions, cfg, clk, authLogger),
		SessionService: newSessionService(users, sessions, authLogger, pub),
		ProfileService: newProfileService(users, cfg, clk, authLogger, rl, issuer),

		keys:        keys,
		rateLimiter: rl,
		pub:         pub,
		clock:       clk,
		issuer:      issuer,
	}
}

// publish es el atajo intra-paquete para llamadas del DeviceCodeService
// y futuros sub-systems del paquete `auth` que quieran emitir eventos
// vía el publisher compartido.
func (s *Service) publish(e event.Event) {
	s.pub.publish(e)
}

// createSession es el atajo intra-paquete al issuer compartido.
// Mantiene la firma histórica del `(s *Service).createSession` para
// que device.go (mismo paquete) siga llamando sin cambios.
func (s *Service) createSession(ctx context.Context, user *authmodel.User, deviceName, deviceID, ip string) (*AuthToken, error) {
	return s.issuer.issue(ctx, user, deviceName, deviceID, ip)
}

// SetEventBus wirea un event bus para que el servicio pueda publicar
// UserLoggedIn / UserLoggedOut. Nil deshabilita publishing. El mismo
// `*publisher` está compartido por LoginService y SessionService así
// un solo Set actualiza ambos a la vez.
func (s *Service) SetEventBus(bus *event.Bus) {
	s.pub.setBus(bus)
}

// KeyStoreOrNil devuelve el signing keystore del servicio. Expuesto
// para el handler admin (rotación manual de keys) y para
// observability; devuelve nil si el servicio se construyó sin uno
// (tests que no tocan tokens).
func (s *Service) KeyStoreOrNil() *KeyStore {
	return s.keys
}

// StopSessionCleaner detiene la goroutine del session cleaner Y el
// rate-limiter. Ambos son background tasks del Service sin lifecycle
// propio; cerrarlos juntos evita el leak documentado como audit olor
// RR (cerrado en iter 1, este método mantiene la coordinación).
//
// Override del método promovido desde SessionService para que también
// pare el rate-limiter (el cual NO vive en SessionService — es shared
// con LoginService y ProfileService).
func (s *Service) StopSessionCleaner() {
	s.SessionService.StopSessionCleaner()
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
}
