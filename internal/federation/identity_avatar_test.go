package federation

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// tinyPNG genera un PNG válido pequeño (4×4 rojo) para alimentar
// imaging.GenerateAvatar sin depender de fixtures en disco.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func newManagerWithDir(t *testing.T, dir string) *Manager {
	t.Helper()
	ctx := context.Background()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "T"); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	cfg := DefaultConfig()
	cfg.AvatarsDir = dir
	mgr, err := NewManager(ctx, cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

// TestUploadIdentityAvatar_RoundTrip cubre el camino feliz:
// upload escribe a disco, persiste el path, y un delete posterior
// limpia ambos. Único test que toca el flujo entero — la validación
// (mime, tamaño, decompression bomb) reusa el mismo pipeline que
// user.UploadAvatar y está cubierta allí.
func TestUploadIdentityAvatar_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mgr := newManagerWithDir(t, dir)
	ctx := context.Background()

	rel, err := mgr.UploadIdentityAvatar(ctx, tinyPNG(t), "image/png")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !strings.HasPrefix(rel, "server-") || !strings.HasSuffix(rel, ".jpg") {
		t.Errorf("relName = %q, want server-<rand>.jpg", rel)
	}
	if got := mgr.IdentityAvatarPath(); got != rel {
		t.Errorf("identity path = %q, want %q", got, rel)
	}
	full := filepath.Join(dir, rel)
	if _, err := os.Stat(full); err != nil {
		t.Errorf("avatar file not written: %v", err)
	}

	// Re-upload reemplaza el fichero y borra el anterior.
	rel2, err := mgr.UploadIdentityAvatar(ctx, tinyPNG(t), "image/png")
	if err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	if rel2 == rel {
		t.Fatal("re-upload produced same relName; cache-buster broken")
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Errorf("previous avatar should be removed; stat err = %v", err)
	}

	// Delete limpia disco + DB.
	if err := mgr.DeleteIdentityAvatar(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := mgr.IdentityAvatarPath(); got != "" {
		t.Errorf("after delete identity path = %q, want empty", got)
	}
	full2 := filepath.Join(dir, rel2)
	if _, err := os.Stat(full2); !os.IsNotExist(err) {
		t.Errorf("avatar file should be removed after delete; stat err = %v", err)
	}
}

// TestUploadIdentityAvatar_DisabledWhenDirEmpty asegura que sin
// AvatarsDir configurado (config :memory: en tests, sin volumen
// persistente) el upload devuelve un error de validación claro que
// el handler mapea a 503.
func TestUploadIdentityAvatar_DisabledWhenDirEmpty(t *testing.T) {
	mgr := newManagerWithDir(t, "")
	_, err := mgr.UploadIdentityAvatar(context.Background(), tinyPNG(t), "image/png")
	if err == nil {
		t.Fatal("expected error when AvatarsDir empty")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("err = %T (%v), want *domain.ValidationError", err, err)
	}
}

// TestDeleteIdentityAvatar_IdempotentNoOp: sin avatar previo el
// delete devuelve nil silenciosamente (mismo contrato que el de
// usuario, para que el handler responda 204 sin distinguir casos).
func TestDeleteIdentityAvatar_IdempotentNoOp(t *testing.T) {
	mgr := newManagerWithDir(t, t.TempDir())
	if err := mgr.DeleteIdentityAvatar(context.Background()); err != nil {
		t.Errorf("delete with no prior avatar should be no-op, got %v", err)
	}
}

// TestIdentityAvatarFilePath_RejectsTraversal pinea el guard:
// nombres con "..", "/" o "" devuelven error en vez de resolver a
// un path fuera del dir. Mirror exacto del check de
// user.Service.AvatarFilePath.
func TestIdentityAvatarFilePath_RejectsTraversal(t *testing.T) {
	mgr := newManagerWithDir(t, t.TempDir())
	cases := []string{"", "..", "../etc/passwd", "sub/dir.jpg", "/abs.jpg"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := mgr.IdentityAvatarFilePath(name); err == nil {
				t.Errorf("expected error for %q", name)
			}
		})
	}
}
