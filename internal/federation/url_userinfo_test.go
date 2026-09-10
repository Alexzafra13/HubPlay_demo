package federation

import (
	"errors"
	"testing"

	"hubplay/internal/domain"
)

// TestValidatePeerURL_RejectsEmbeddedCredentials (F-10): `user:pass@host`
// no tiene sentido en una URL de peer (la auth es JWT firmado) y acabaría
// en logs y en la tabla de peers.
func TestValidatePeerURL_RejectsEmbeddedCredentials(t *testing.T) {
	t.Parallel()
	for _, u := range []string{
		"https://alice:secret@example.com",
		"http://token@192.168.1.10:8096",
	} {
		if err := validatePeerURL(u); !errors.Is(err, domain.ErrPeerURLUnsafe) {
			t.Errorf("%s: want ErrPeerURLUnsafe, got %v", u, err)
		}
	}
}
