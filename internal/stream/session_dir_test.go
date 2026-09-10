package stream

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSessionDirName_IsCreatableOnEveryOS: la clave de sesión lleva ':'
// (inválido en nombres de fichero en Windows). El nombre en disco no debe
// contener separadores prohibidos y debe poder crearse de verdad.
func TestSessionDirName_IsCreatableOnEveryOS(t *testing.T) {
	key := sessionKey("user-1", "item-2", "720p", -1, -1)
	name := sessionDirName(key)
	if name == "" || name == key && filepath.Separator == '\\' {
		t.Fatalf("dir name %q must differ from raw key on Windows", name)
	}
	for _, bad := range []string{":", "/", "\\", "<", ">", "\"", "|", "?", "*"} {
		if filepath.Base(name) != name || containsAny(name, bad) {
			t.Fatalf("dir name %q contains forbidden %q", name, bad)
		}
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	// Dos claves distintas no colisionan en disco.
	other := sessionDirName(sessionKey("user-1", "item-2", "720p", 1, -1))
	if other == name {
		t.Fatal("different keys must map to different dir names")
	}
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, r := range s {
			if r == c {
				return true
			}
		}
	}
	return false
}
