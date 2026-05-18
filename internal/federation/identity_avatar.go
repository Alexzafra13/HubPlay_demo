package federation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hubplay/internal/domain"
	"hubplay/internal/imaging"
)

// Mismas constantes que user.Service para mantener un único contrato
// con el frontend (que valida mime + tamaño antes del round-trip).
// 5 MB cubre fotos de móvil sin abrir la puerta a payloads pensados
// para consumir memoria decodificando.
const (
	IdentityAvatarMaxBytes = 5 * 1024 * 1024
	IdentityAvatarSize     = 256
)

// identityAvatarPrefix etiqueta el fichero en disco para que se vea
// de un vistazo qué es y para que un loop por nombres no lo confunda
// con un avatar de usuario. Los UUIDs de usuario nunca empiezan por
// "server-", así que el namespace es disjunto.
const identityAvatarPrefix = "server"

// IdentityAvatarFilePath devuelve la ruta absoluta del fichero en
// disco a partir del nombre relativo persistido en DB. Comprueba que
// el resultado quede contenido en avatarsDir para que ni un valor
// inesperado en DB ni un path-traversal explícito puedan escapar del
// directorio. Mirror exacto de user.Service.AvatarFilePath.
func (m *Manager) IdentityAvatarFilePath(relName string) (string, error) {
	if m.avatarsDir == "" {
		return "", fmt.Errorf("federation: avatars dir not configured")
	}
	if !imaging.IsSafePathSegment(relName) {
		return "", fmt.Errorf("federation: invalid avatar filename")
	}
	full := filepath.Join(m.avatarsDir, relName)
	rel, err := filepath.Rel(m.avatarsDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("federation: avatar path escapes dir")
	}
	return full, nil
}

// UploadIdentityAvatar valida + procesa + persiste la foto del
// servidor. Mismo pipeline que user.Service.UploadAvatar — los
// peers consumen el resultado vía /federation/identity/avatar.
//
//  1. Tamaño + MIME (jpeg/png/webp) antes de gastar memoria.
//  2. Decompression-bomb guard (EnforceMaxPixels).
//  3. Decode + center-crop cuadrado + resize 256×256 JPEG q85.
//  4. AtomicWriteFile en avatarsDir/server-<rand8>.jpg para que el
//     swap sea atómico y el sufijo aleatorio funcione de cache-buster
//     en los peers (el path cambia y refetchean sin negociar ETag).
//  5. Borra el anterior best-effort (si falla queda un huérfano,
//     no rompe nada).
//  6. Persiste el nuevo path en DB vía IdentityStore.
//
// Devuelve la ruta relativa para que el handler la incluya en la
// respuesta sin re-leer del store.
func (m *Manager) UploadIdentityAvatar(ctx context.Context, data []byte, contentType string) (string, error) {
	if m.avatarsDir == "" {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "server avatar uploads are disabled on this server",
		})
	}
	if len(data) == 0 {
		return "", domain.NewValidationError(map[string]string{
			"avatar": "empty file",
		})
	}
	if len(data) > IdentityAvatarMaxBytes {
		return "", domain.NewValidationError(map[string]string{
			"avatar": fmt.Sprintf("file too large (max %d bytes)", IdentityAvatarMaxBytes),
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

	resized, err := imaging.GenerateAvatar(data, IdentityAvatarSize)
	if err != nil {
		return "", fmt.Errorf("federation: process avatar: %w", err)
	}

	if err := os.MkdirAll(m.avatarsDir, 0o755); err != nil {
		return "", fmt.Errorf("federation: ensure avatars dir: %w", err)
	}

	// Sufijo aleatorio = cache-buster: cada upload genera un nombre
	// nuevo, el path público que publicamos en /federation/info
	// cambia y los peers refetchean sin tener que negociar ETag.
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("federation: avatar suffix: %w", err)
	}
	relName := fmt.Sprintf("%s-%s.jpg", identityAvatarPrefix, hex.EncodeToString(suffix[:]))
	full := filepath.Join(m.avatarsDir, relName)

	if err := imaging.AtomicWriteFile(full, resized, 0o644); err != nil {
		return "", fmt.Errorf("federation: write avatar: %w", err)
	}

	// Best-effort cleanup del anterior. Si falla, el avatar nuevo ya
	// está guardado y el path en DB se actualizará igual; queda un
	// fichero huérfano hasta el próximo upload (que también intentará
	// borrarlo) o un GC manual.
	if prev := m.identity.Current(); prev != nil && prev.AvatarImagePath != "" && prev.AvatarImagePath != relName {
		if oldFull, err := m.IdentityAvatarFilePath(prev.AvatarImagePath); err == nil {
			_ = os.Remove(oldFull)
		}
	}

	if err := m.identity.SetAvatarPath(ctx, relName); err != nil {
		// Si el persist falla tras escribir el fichero, limpiamos el
		// huérfano que acabamos de crear para no acumular.
		_ = os.Remove(full)
		return "", fmt.Errorf("federation: persist avatar path: %w", err)
	}
	m.logger.Info("server avatar uploaded", "bytes", len(resized), "file", relName)
	return relName, nil
}

// DeleteIdentityAvatar borra el fichero + limpia el path en DB.
// Idempotente: no-op si no había avatar previo. Mirror exacto de
// user.Service.DeleteAvatar.
func (m *Manager) DeleteIdentityAvatar(ctx context.Context) error {
	if m.avatarsDir == "" {
		// Sin dir configurado no había avatares; nada que hacer.
		return nil
	}
	prev := m.identity.Current()
	if prev == nil || prev.AvatarImagePath == "" {
		return nil
	}
	if full, err := m.IdentityAvatarFilePath(prev.AvatarImagePath); err == nil {
		// Best-effort: si el fichero ya no está, seguimos limpiando DB.
		_ = os.Remove(full)
	}
	if err := m.identity.SetAvatarPath(ctx, ""); err != nil {
		return fmt.Errorf("federation: clear avatar path: %w", err)
	}
	m.logger.Info("server avatar removed")
	return nil
}
