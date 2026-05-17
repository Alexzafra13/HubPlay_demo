package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/imaging"
)

// AvatarMaxBytes: tope del payload aceptado en POST /me/avatar. 5 MB
// cubre fotos de móvil y deja margen, pero corta de raíz un payload
// pensado para tumbar el servidor antes de gastar memoria decodificándolo.
const AvatarMaxBytes = 5 * 1024 * 1024

// AvatarSize: lado del cuadrado al que normalizamos cada avatar.
// 256 px sobra para todos los sitios donde se renderiza (lista admin,
// TopBar, Mi cuenta, picker de perfiles). Los frontends piden el
// mismo recurso a todos los tamaños y dejan que el navegador escale —
// no servimos múltiples resoluciones.
const AvatarSize = 256

type Service struct {
	users      *db.UserRepository
	logger     *slog.Logger
	avatarsDir string
}

// NewService recibe el directorio donde se persisten los avatares
// subidos. Por convención vive junto a la DB (config/avatars/) para
// que el mismo volumen docker cubra ambos. Vacío = subida deshabilitada
// (el handler 503'a en ese caso).
func NewService(users *db.UserRepository, logger *slog.Logger, avatarsDir string) *Service {
	return &Service{
		users:      users,
		logger:     logger.With("module", "user"),
		avatarsDir: avatarsDir,
	}
}

func (s *Service) GetByID(ctx context.Context, id string) (*authmodel.User, error) {
	return s.users.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.users.List(ctx, limit, offset)
}

func (s *Service) Update(ctx context.Context, u *authmodel.User) error {
	if err := s.users.Update(ctx, u); err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	s.logger.Info("user updated", "user_id", u.ID)
	return nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.users.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	s.logger.Info("user deleted", "user_id", id)
	return nil
}

// Count: lo usa el wizard de setup para saber si toca crear el primer admin.
func (s *Service) Count(ctx context.Context) (int, error) {
	return s.users.Count(ctx)
}

// SetMaxContentRating: cap de contenido por perfil. "" = sin restricción.
func (s *Service) SetMaxContentRating(ctx context.Context, id, rating string) error {
	if err := s.users.SetMaxContentRating(ctx, id, rating); err != nil {
		return fmt.Errorf("set max content rating: %w", err)
	}
	s.logger.Info("max content rating set", "user_id", id, "rating", rating)
	return nil
}

// allowedAvatarHexes: paleta de 8 entradas replicada en web/src/utils/avatarColor.ts.
// Validación server-side para que un frontend rogue no escriba hex arbitrario.
// Reducida desde 14 a 8 colores claramente distintos (antes había pares casi
// idénticos como moss/olive, terracotta/garnet, navy/slate/petrol) para que el
// picker ofrezca opciones que se distinguen de un vistazo en lugar de variantes
// del mismo tono. Mantener en lock-step con el frontend.
var allowedAvatarHexes = map[string]struct{}{
	"#b91c1c": {}, // rojo
	"#c2410c": {}, // naranja
	"#a16207": {}, // ámbar
	"#15803d": {}, // verde
	"#0f766e": {}, // turquesa
	"#1d4ed8": {}, // azul
	"#6d28d9": {}, // violeta
	"#be185d": {}, // rosa
}

// SetAvatarColor: "" = limpia override (frontend cae al helper FNV-1a → paleta).
// Cualquier hex fuera de las 14 entradas conocidas es 400.
func (s *Service) SetAvatarColor(ctx context.Context, id, hex string) error {
	trimmed := strings.TrimSpace(strings.ToLower(hex))
	if trimmed != "" {
		if _, ok := allowedAvatarHexes[trimmed]; !ok {
			return domain.NewValidationError(map[string]string{
				"avatar_color": "must be empty or one of the known palette colours",
			})
		}
	}
	if err := s.users.SetAvatarColor(ctx, id, trimmed); err != nil {
		return fmt.Errorf("set avatar color: %w", err)
	}
	s.logger.Info("avatar color updated", "user_id", id, "color", trimmed)
	return nil
}

// SetDisplayName: sólo cambia la etiqueta humana; username + parent_user_id intactos.
// Validación en el service (1..64 sin whitespace) y no en el repo, así callers
// confiables pueden escribir directo sin pasar por la validación.
func (s *Service) SetDisplayName(ctx context.Context, id, name string) error {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) == 0 || len(trimmed) > 64 {
		return domain.NewValidationError(map[string]string{
			"display_name": "must be 1-64 characters",
		})
	}
	if err := s.users.SetDisplayName(ctx, id, trimmed); err != nil {
		return fmt.Errorf("set display name: %w", err)
	}
	s.logger.Info("display name updated", "user_id", id)
	return nil
}

// SetRole: promueve/degrada entre "user" y "admin". El gate del primary-admin
// vive en el handler, no aquí.
func (s *Service) SetRole(ctx context.Context, id, role string) error {
	if err := s.users.SetRole(ctx, id, role); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	s.logger.Info("user role changed", "user_id", id, "role", role)
	return nil
}

// SetActive: false → login rechazado y middleware JWT rechaza requests siguientes.
// Row no se borra: re-activar restaura todo.
func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	if err := s.users.SetActive(ctx, id, active); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	s.logger.Info("user active state changed", "user_id", id, "active", active)
	return nil
}

// PrimaryAdminID: id del admin más antiguo. El admin UI lo usa para bloquear
// acciones destructivas contra el bootstrap admin.
func (s *Service) PrimaryAdminID(ctx context.Context) (string, error) {
	return s.users.PrimaryAdminID(ctx)
}

// SetAccessExpiresAt: nil = acceso permanente. Login + middleware rechazan
// tras este stamp.
func (s *Service) SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error {
	if err := s.users.SetAccessExpiresAt(ctx, id, expiresAt); err != nil {
		return fmt.Errorf("set access expires at: %w", err)
	}
	if expiresAt == nil {
		s.logger.Info("user access set to permanent", "user_id", id)
	} else {
		s.logger.Info("user access window set", "user_id", id, "expires_at", *expiresAt)
	}
	return nil
}

// AvatarsDir devuelve el directorio donde se persisten los avatares.
// Vacío = la feature está deshabilitada (NewService recibió "").
func (s *Service) AvatarsDir() string { return s.avatarsDir }

// AvatarFilePath devuelve la ruta absoluta en disco para un nombre
// de fichero relativo. Comprueba que el resultante esté contenido
// en avatarsDir para evitar path traversal aunque la DB acabara
// con un valor inesperado.
func (s *Service) AvatarFilePath(relName string) (string, error) {
	if s.avatarsDir == "" {
		return "", fmt.Errorf("avatars dir not configured")
	}
	if !imaging.IsSafePathSegment(relName) {
		return "", fmt.Errorf("invalid avatar filename")
	}
	full := filepath.Join(s.avatarsDir, relName)
	rel, err := filepath.Rel(s.avatarsDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("avatar path escapes dir")
	}
	return full, nil
}

// UploadAvatar valida + procesa + persiste el avatar de un usuario.
// El flujo:
//  1. Tamaño y MIME (jpeg/png/webp) — corta payloads no-imagen antes
//     de gastar memoria decodificando.
//  2. Decompression-bomb guard (EnforceMaxPixels) sobre los bytes
//     crudos: rechaza imágenes que prometan > 40 MP en su header.
//  3. GenerateAvatar: decode + center-crop cuadrado + resize 256×256
//     + encode JPEG q85. Resultado tiípicamente 10-20 KB.
//  4. AtomicWriteFile en avatarsDir/<userId>-<rand8>.jpg para que
//     el cambio sea atómico y sirva de cache-buster (el nombre cambia
//     en cada upload, así que la URL pública cambia y peers/browsers
//     refetchean sin ETag).
//  5. Borra el anterior (best-effort — un fichero huérfano no rompe
//     nada, sólo ocupa espacio hasta el próximo upload o el GC manual).
//  6. Persiste la nueva ruta relativa en DB.
//
// Devuelve la ruta relativa guardada para que el handler la incluya
// en la respuesta sin re-leer del DB.
func (s *Service) UploadAvatar(ctx context.Context, userID string, data []byte, contentType string) (string, error) {
	if s.avatarsDir == "" {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "avatar uploads are disabled on this server",
		})
	}
	if len(data) == 0 {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "empty file",
		})
	}
	if len(data) > AvatarMaxBytes {
		return "", domain.NewValidationError(map[string]string{
			"avatar": fmt.Sprintf("file too large (max %d bytes)", AvatarMaxBytes),
		})
	}
	if !imaging.IsValidContentType(contentType) {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "unsupported image type — use JPEG, PNG or WebP",
		})
	}
	if err := imaging.EnforceMaxPixels(data); err != nil {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "image too large to decode safely",
		})
	}

	resized, err := imaging.GenerateAvatar(data, AvatarSize)
	if err != nil {
		return "", fmt.Errorf("process avatar: %w", err)
	}

	if err := os.MkdirAll(s.avatarsDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure avatars dir: %w", err)
	}

	// Sufijo aleatorio de 8 hex chars = cache-buster cuando el
	// usuario re-sube su avatar. El nombre nuevo se persiste en
	// DB, así que la URL pública cambia y el navegador refetchea.
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("avatar suffix: %w", err)
	}
	relName := fmt.Sprintf("%s-%s.jpg", userID, hex.EncodeToString(suffix[:]))
	full := filepath.Join(s.avatarsDir, relName)

	if err := imaging.AtomicWriteFile(full, resized, 0o644); err != nil {
		return "", fmt.Errorf("write avatar: %w", err)
	}

	// Best-effort cleanup del anterior. Si falla por permisos / IO,
	// el avatar nuevo ya está guardado y la DB se actualizará igual;
	// queda un fichero huérfano hasta el próximo upload (que también
	// intentará borrarlo) o un GC manual.
	prev, _ := s.users.GetByID(ctx, userID)
	if prev != nil && prev.AvatarPath != "" && prev.AvatarPath != relName {
		if oldFull, err := s.AvatarFilePath(prev.AvatarPath); err == nil {
			_ = os.Remove(oldFull)
		}
	}

	if err := s.users.SetAvatarPath(ctx, userID, relName); err != nil {
		// Si la DB falla tras escribir el fichero, intentamos limpiar
		// el huérfano que acabamos de crear; sin esto se acumulan.
		_ = os.Remove(full)
		return "", fmt.Errorf("persist avatar path: %w", err)
	}
	s.logger.Info("avatar uploaded", "user_id", userID, "bytes", len(resized), "file", relName)
	return relName, nil
}

// DeleteAvatar borra el avatar subido (fichero + DB). No-op si no
// había uno; idempotente.
func (s *Service) DeleteAvatar(ctx context.Context, userID string) error {
	if s.avatarsDir == "" {
		// Sin dir configurado no había avatares; nada que hacer.
		return nil
	}
	prev, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user before avatar delete: %w", err)
	}
	if prev == nil || prev.AvatarPath == "" {
		return nil
	}
	if full, err := s.AvatarFilePath(prev.AvatarPath); err == nil {
		// Best-effort: si el fichero ya no está, seguimos limpiando DB.
		_ = os.Remove(full)
	}
	if err := s.users.ClearAvatarPath(ctx, userID); err != nil {
		return fmt.Errorf("clear avatar path: %w", err)
	}
	s.logger.Info("avatar removed", "user_id", userID)
	return nil
}
