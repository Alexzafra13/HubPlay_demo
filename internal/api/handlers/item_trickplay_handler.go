package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hubplay/internal/imaging"

	"github.com/go-chi/chi/v5"
)

// TrickplayHandler aísla la generación y serving de sprite-sheets de
// trickplay (hover-preview en la seek bar). Lleva su propio estado
// concurrencia (sync.Map de locks por item + WaitGroup para drenar
// goroutines de generación en tests) que NO comparte con el resto de
// ItemHandler — separarlo cierra la mitad de "trickplay state" del
// olor P del audit 2026-05-14.
type TrickplayHandler struct {
	lib    LibraryService
	logger *slog.Logger
	// trickplayDir is the root for generated trickplay sprites
	// (`<dir>/<itemID>/sprite.png` + `manifest.json`). Empty disables
	// the feature; los endpoints devuelven 503 en ese caso.
	trickplayDir string
	// trickplayLocks serialises generation per item so a second hover
	// while the first is still running waits instead of double-spawning
	// ffmpeg. El map crece por entrada por item generado; bounded por
	// library size, fine in practice.
	trickplayLocks sync.Map
	// trickplayBG tracks background generation goroutines spawned by
	// ensureTrickplay. Existe sólo para que los tests puedan esperar
	// a que el trabajo in-flight termine antes de que t.Cleanup llame
	// a RemoveAll sobre la TempDir — sin esto, la goroutine sigue
	// escribiendo después de que el test return y el cleanup race
	// contra los writes ("directory not empty" unlinkat error). El
	// shutdown de producción no espera actualmente esto; el ctx
	// cancelado dentro de la goroutine acota el trabajo a su propio
	// deadline.
	trickplayBG sync.WaitGroup
}

func newTrickplayHandler(lib LibraryService, trickplayDir string, logger *slog.Logger) *TrickplayHandler {
	return &TrickplayHandler{
		lib:          lib,
		logger:       logger,
		trickplayDir: trickplayDir,
	}
}

// TrickplayManifest serves the sprite-sheet manifest for an item. El
// manifest le dice al cliente cómo computar qué sub-imagen del sprite
// cubre un tiempo de playback dado. Ver `imaging.TrickplayManifest`
// para el contrato exacto de los campos.
//
// Generación asíncrona: un cache miss arranca ffmpeg en una goroutine
// background y devuelve 503 + Retry-After inmediato, así el HTTP
// request nunca bloquea behind ffmpeg (30-90 s, que antes timeouteaba
// el reverse-proxy a 60 s y aparecía como 504 en el player). El
// `useTrickplay` del frontend ya trata non-200 como "preview
// unavailable" y degrada gracefully — al siguiente render, una vez
// que la goroutine escribió cache, el manifest sirve limpio.
func (h *TrickplayHandler) TrickplayManifest(w http.ResponseWriter, r *http.Request) {
	if h.trickplayDir == "" {
		respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	itemDir, err := h.ensureTrickplay(r.Context(), id)
	if err != nil {
		if errors.Is(err, errTrickplayPending) {
			w.Header().Set("Retry-After", "10")
			respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_PENDING",
				"trickplay sprite is being generated; retry shortly")
			return
		}
		handleServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	http.ServeFile(w, r, filepath.Join(itemDir, "manifest.json"))
}

// TrickplaySprite serves the sprite PNG. Mirrors TrickplayManifest's
// async semantics: cache hit serves immediately; cache miss returns
// 503 with Retry-After while the background ffmpeg run completes.
// Browsers cache the PNG aggressively (same item + same params
// produces byte-identical output) so once it lands the hover-scroll
// is one fetch per long-term cache window.
func (h *TrickplayHandler) TrickplaySprite(w http.ResponseWriter, r *http.Request) {
	if h.trickplayDir == "" {
		respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	itemDir, err := h.ensureTrickplay(r.Context(), id)
	if err != nil {
		if errors.Is(err, errTrickplayPending) {
			w.Header().Set("Retry-After", "10")
			respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_PENDING",
				"trickplay sprite is being generated; retry shortly")
			return
		}
		handleServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	http.ServeFile(w, r, filepath.Join(itemDir, "sprite.png"))
}

// errTrickplayPending es el sentinel que ensureTrickplay devuelve
// cuando una generación background está en vuelo (o recién arrancada).
// Los handlers lo traducen a 503 + Retry-After para que el cliente
// poolee sin bloquear el HTTP request behind ffmpeg.
var errTrickplayPending = errors.New("trickplay: generation pending")

// WaitTrickplayInflight bloquea hasta que cada goroutine background de
// trickplay arrancada vía este handler haya retornado. Pensado para
// tests que usan `t.TempDir()` como trickplay root — sin esto, el test
// return race con goroutines que aún escriben en el dir y el
// `t.Cleanup`'s RemoveAll falla con "directory not empty". Seguro de
// llamar concurrente y desde paths de shutdown de producción si se
// desea drain graceful allí (no hoy).
func (h *TrickplayHandler) WaitTrickplayInflight() {
	h.trickplayBG.Wait()
}

// ensureTrickplay devuelve el directorio per-item conteniendo
// `sprite.png` + `manifest.json` cuando el cache está fresco. Cuando
// el cache falta o está stale, arranca ffmpeg en una goroutine
// background y devuelve errTrickplayPending inmediato — el HTTP
// request del caller NO debe bloquear behind el run de ffmpeg de
// 30-90 s.
//
// Invalidación de cache stale: el manifest cacheado lleva un stamp
// `version` matcheando `imaging.TrickplayManifestVersion`. Cuando el
// contrato del generator cambia (p.ej. v1 hardcoded a 10×10 grid que
// capaba coverage a 1000 s; v2 sizes adaptivamente al runtime del
// item) detectamos el stamp viejo y regeneramos el sprite. Sin este
// gate, servers actualizados servirían thumbnails wrong para cada
// item ingestado antes del upgrade.
//
// Concurrencia: trickplayLocks es sync.Map de itemID → *sync.Mutex.
// El primer request que aterrice en un cache-miss para un item
// TryLockea el mutex, spawnea la goroutine, y la goroutine Unlockea
// cuando ffmpeg termina. Concurrent requests durante la generación
// ven TryLock fail y devuelven pending también — no duplicate
// ffmpegs, no thundering herd.
func (h *TrickplayHandler) ensureTrickplay(ctx context.Context, itemID string) (string, error) {
	itemDir := filepath.Join(h.trickplayDir, itemID)
	spritePath := filepath.Join(itemDir, "sprite.png")
	manifestPath := filepath.Join(itemDir, "manifest.json")

	// Fast path: ambos ficheros ya cacheados Y el manifest version
	// matchea lo que el generator actual produce. Version mismatch
	// (o manifest unreadable / missing-field) cae al kickoff de
	// regeneración abajo.
	if trickplayCacheFresh(spritePath, manifestPath) {
		return itemDir, nil
	}

	// Mutex per-item via sync.Map. TryLock means: si otro caller ya
	// está generando (o just-about-to), no queueamos behind él — le
	// decimos a nuestro caller "pending" también. Retryarán shortly,
	// y cuando la generación land, el fast path arriba toma over.
	mu, _ := h.trickplayLocks.LoadOrStore(itemID, &sync.Mutex{})
	lock := mu.(*sync.Mutex)
	if !lock.TryLock() {
		return "", errTrickplayPending
	}

	// Re-check bajo el lock — un holder previo puede haber terminado
	// mientras entrábamos en esta branch. Releaseamos el lock antes
	// de retornar para que quede available para futuras
	// invalidaciones legítimas.
	if trickplayCacheFresh(spritePath, manifestPath) {
		lock.Unlock()
		return itemDir, nil
	}

	item, err := h.lib.GetItem(ctx, itemID)
	if err != nil {
		lock.Unlock()
		return "", err
	}
	if item.Path == "" {
		lock.Unlock()
		return "", errors.New("item has no playable file path")
	}

	// Duration plumbed en segundos para que el generator pueda elegir
	// un interval+grid adaptivo que cubra TODA la timeline. Items
	// guardan runtime en ticks 100-ns (Jellyfin convention); 0
	// significa que el scanner aún no lo probó, en cuyo caso el
	// generator cae a su legacy 10×10 = 1000 s coverage.
	durationSec := float64(0)
	if item.DurationTicks > 0 {
		durationSec = float64(item.DurationTicks) / 10_000_000.0
	}
	itemPath := item.Path

	// Spawn el run real de ffmpeg en una goroutine fresca con ctx
	// fresco — usar r.Context() mataría la generación en cuanto el
	// cliente timeoutea / desconecta. El lock se libera dentro de la
	// goroutine cuando el trabajo termina (success o fail).
	h.trickplayBG.Add(1)
	go func() {
		defer h.trickplayBG.Done()
		defer lock.Unlock()
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		params := imaging.TrickplayParams{DurationSeconds: durationSec}
		if _, err := imaging.GenerateTrickplayWithDeadline(bgCtx, itemPath, itemDir, params, 0); err != nil {
			h.logger.Warn("trickplay generation failed (background)",
				"item_id", itemID, "error", err)
		}
	}()

	return "", errTrickplayPending
}

// trickplayCacheFresh reporta si el sprite + manifest cacheados para
// un item son usables tal cual. Devuelve false cuando cualquiera de
// los ficheros falta O cuando el stamp `version` del manifest lagea
// el contrato actual del generator (TrickplayManifestVersion).
// Decoded como struct bare para que un manifest unreadable /
// partially-written también aterrice en "regenerate" en lugar de
// servir garbage.
func trickplayCacheFresh(spritePath, manifestPath string) bool {
	if _, err := os.Stat(spritePath); err != nil {
		return false
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	var m imaging.TrickplayManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	return m.Version >= imaging.TrickplayManifestVersion
}
