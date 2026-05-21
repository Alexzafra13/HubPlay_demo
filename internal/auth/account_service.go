package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// AccountService cubre el ciclo de vida de la CUENTA (no de sesiones):
// `Register`, `ResetPassword` (admin-driven) y `ChangePassword` (user-
// driven self-service). Toca `users` para mutar el row y `sessions`
// sólo para invalidar tras un reset administrativo (defensa contra
// tokens vivos del usuario antiguo).
type AccountService struct {
	users    *db.UserRepository
	sessions *db.SessionRepository
	cfg      config.AuthConfig
	clock    clock.Clock
	logger   *slog.Logger
}

func newAccountService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
) *AccountService {
	return &AccountService{
		users:    users,
		sessions: sessions,
		cfg:      cfg,
		clock:    clk,
		logger:   logger,
	}
}

// Register crea una cuenta nueva (o un profile hijo si
// `req.ParentUserID` viene set). El bcrypt cost lo define la config.
func (s *AccountService) Register(ctx context.Context, req RegisterRequest) (*authmodel.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BCryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	role := req.Role
	if role == "" {
		role = "user"
	}

	user := &authmodel.User{
		ID:                     uuid.New().String(),
		Username:               req.Username,
		DisplayName:            req.DisplayName,
		PasswordHash:           string(hash),
		Role:                   role,
		IsActive:               true,
		CreatedAt:              s.clock.Now(),
		ParentUserID:           req.ParentUserID,
		PasswordChangeRequired: req.PasswordChangeRequired,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	s.logger.Info("user registered", "user_id", user.ID, "username", user.Username, "role", role)
	return user, nil
}

// ResetPassword es el path admin-only "el usuario perdió su clave".
// Genera una clave readable nueva, la hashea, la guarda con
// must_change=true, y devuelve el plaintext exactamente UNA vez. El
// handler es responsable de surfaceárselo al admin — esta función
// nunca vuelve a tocar la DB con el plaintext, así un leak aquí es
// equivalente al leak de una clave que el admin tipease en un form.
//
// Todas las sesiones del target se invalidan para que un JWT robado
// para la clave vieja se vuelva worthless inmediatamente.
func (s *AccountService) ResetPassword(ctx context.Context, userID string) (string, error) {
	plain, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), s.cfg.BCryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	if err := s.users.SetPassword(ctx, userID, string(hash), true); err != nil {
		return "", err
	}
	if _, err := s.sessions.DeleteAllByUser(ctx, userID); err != nil {
		// Logged pero no fatal — la clave ya rotó, el peor caso es
		// que el refresh viejo sobreviva el JWT TTL hasta el próximo
		// intento de refresh.
		s.logger.Warn("reset password: invalidate sessions failed", "user_id", userID, "error", err)
	}
	s.logger.Info("admin reset password", "user_id", userID)
	return plain, nil
}

// ChangePassword es el path user-side "rotar mi propia clave".
// Verifica la clave actual antes de mutar para que un JWT robado
// no pueda pivotar a un takeover permanente. Limpia el flag
// must_change — así una rotación forzada se completa.
func (s *AccountService) ChangePassword(ctx context.Context, userID, current, next string) error {
	if next == "" || len(next) < 8 {
		return fmt.Errorf("change password: %w", domain.ErrInvalidPassword)
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("change password lookup: %w", err)
	}
	// Skip current-password check cuando must_change está set Y el
	// caller no lo suministró. Matchea el UX de "acabas de tipear
	// la clave auto-generada para entrar, no tienes que tiparla otra
	// vez" — pero si `current` viene set, lo verificamos como belt-
	// and-braces. De Morgan'd del obvio `!(must_change && current=="")`
	// para que staticcheck QF1001 quede quieto.
	if !user.PasswordChangeRequired || current != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
			return fmt.Errorf("change password: %w", domain.ErrInvalidPassword)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cfg.BCryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	if err := s.users.SetPassword(ctx, userID, string(hash), false); err != nil {
		return err
	}
	s.logger.Info("password changed", "user_id", userID)
	return nil
}

// passwordAlphabet es el charset readable para claves admin-generadas.
// Drop de 0/O/1/l/I para que sea copy-pasteable sin friction
// ("¿es un uno o una L?"). El admin lee la clave al usuario, así que
// la legibilidad importa.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword devuelve una clave readable de 12 chars desde un
// CSPRNG. Length 12 sobre 56-char alphabet ≈ 70 bits de entropía —
// cómodo para una credential temporal que el user rotará al primer
// login. El bias del `% len(alphabet)` es despreciable a esta length.
func GeneratePassword() (string, error) {
	const n = 12
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating password: %w", err)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(out), nil
}
