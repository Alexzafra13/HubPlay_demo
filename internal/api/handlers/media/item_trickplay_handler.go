package media

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

	"hubplay/internal/api/handlers"
	"hubplay/internal/imaging"
	librarymodel "hubplay/internal/library/model"
)

// TrickplayHandler aísla la generación y serving de sprite-sheets de
// trickplay (hover-preview en la seek bar). Lleva su propio estado
// concurrencia (sync.Map de locks por item + WaitGroup para drenar
// goroutines de generación en tests) que NO comparte con el resto de
// ItemHandler — separarlo cierra la mitad de "trickplay state" del
// olor P del audit 2026-05-14.
// trickplayItemLookup es el contrato mínimo: buscar un item para
// resolver su path en disco antes de generar el sprite.
type trickplayItemLookup interface {
	GetItem(ctx context.Context, id string) (*librarymodel.Item, error)
}

type TrickplayHandler struct {
	lib    trickplayItemLookup
	access LibraryACL
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
	// genSlots limita los ffmpeg de generación CONCURRENTES a nivel
	// global. El lock per-item solo evita duplicados del mismo item:
	// sin este semáforo, hoverear una fila de la home lanzaba N ffmpegs
	// de hasta 180s a la vez y ponía el servidor de rodillas — los
	// transcodes activos incluidos. PB-12 (audit 2026-06-10).
	genSlots chan struct{}
}

func newTrickplayHandler(lib trickplayItemLookup, access LibraryACL, trickplayDir string, logger *slog.Logger) *TrickplayHandler {
	return &TrickplayHandler{
		lib:          lib,
		access:       access,
		logger:       logger,
		trickplayDir: trickplayDir,
		genSlots:     make(chan struct{}, 2),
	}
}

// authorizeTrickplayItem resuelve el item y aplica el ACL de biblioteca
// con respuesta 404 anti-enumeración — mismo contrato que
// StreamHandler.authorizeItem. Antes trickplay era el único surface de
// playback SIN gate: cualquier usuario autenticado podía ver la
// timeline visual completa (200 frames de la película) de bibliotecas
// a las que no tiene acceso. PB-12 (audit 2026-06-10).
func (h *TrickplayHandler) authorizeTrickplayItem(w http.ResponseWriter, r *http.Request, itemID string) (*librarymodel.Item, bool) {
	item, err := h.lib.GetItem(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return nil, false
	}
	if h.access != nil && !handlers.CanAccessLibrary(r, h.access, h.logger, item.LibraryID) {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
		return nil, false
	}
	return item, true
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
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	item, ok := h.authorizeTrickplayItem(w, r, id)
	if !ok {
		return
	}
	itemDir, err := h.ensureTrickplay(item)
	if err != nil {
		h.respondTrickplayError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", handlers.CacheControlImage)
	http.ServeFile(w, r, filepath.Join(itemDir, "manifest.json"))
}

// respondTrickplayError mapea los sentinels de ensureTrickplay:
// pending → 503 + Retry-After (el cliente poolea); failed → 404 SIN
// Retry-After, para que el frontend deje de poolear y no relance
// ffmpeg en bucle contra un fichero que no genera (PB-13).
func (h *TrickplayHandler) respondTrickplayError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errTrickplayPending):
		w.Header().Set("Retry-After", "10")
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_PENDING",
			"trickplay sprite is being generated; retry shortly")
	case errors.Is(err, errTrickplayFailed):
		handlers.RespondError(w, r, http.StatusNotFound, "TRICKPLAY_UNAVAILABLE",
			"trickplay generation failed for this item")
	default:
		handlers.HandleServiceError(w, r, err)
	}
}

// TrickplaySprite serves the sprite PNG. Mirrors TrickplayManifest's
// async semantics: cache hit serves immediately; cache miss returns
// 503 with Retry-After while the background ffmpeg run completes.
// Browsers cache the PNG aggressively (same item + same params
// produces byte-identical output) so once it lands the hover-scroll
// is one fetch per long-term cache window.
func (h *TrickplayHandler) TrickplaySprite(w http.ResponseWriter, r *http.Request) {
	if h.trickplayDir == "" {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	item, ok := h.authorizeTrickplayItem(w, r, id)
	if !ok {
		return
	}
	itemDir, err := h.ensureTrickplay(item)
	if err != nil {
		h.respondTrickplayError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", handlers.CacheControlImage)
	http.ServeFile(w, r, filepath.Join(itemDir, "sprite.png"))
}

// errTrickplayPending es el sentinel que ensureTrickplay devuelve
// cuando una generación background está en vuelo (o recién arrancada).
// Los handlers lo traducen a 503 + Retry-After para que el cliente
// poolee sin bloquear el HTTP request behind ffmpeg.
var errTrickplayPending = errors.New("trickplay: generation pending")

// errTrickplayFailed es el sentinel para items con un intento de
// generación fallido reciente (negative cache). Sin él, cada hover
// relanzaba OTRO ffmpeg de hasta 180s contra el mismo fichero
// corrupto, en bucle con el Retry-After del polling del frontend.
// PB-13 (audit 2026-06-10).
var errTrickplayFailed = errors.New("trickplay: generation failed recently")

// trickplayFailedTTL es cuánto se respeta el marcador de fallo antes
// de permitir un reintento (el fichero puede haber sido reemplazado).
const trickplayFailedTTL = 24 * time.Hour

// trickplayFailedMarker es el nombre del fichero-marcador dentro del
// dir per-item. Su mtime es el timestamp del fallo.
const trickplayFailedMarker = "failed.marker"

// trickplayFailedRecently reporta si hay un marcador de fallo dentro
// del TTL para el dir del item.
func trickplayFailedRecently(itemDir string) bool {
	info, err := os.Stat(filepath.Join(itemDir, trickplayFailedMarker))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < trickplayFailedTTL
}

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
func (h *TrickplayHandler) ensureTrickplay(item *librarymodel.Item) (string, error) {
	itemID := item.ID
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

	// Negative cache: un intento fallido reciente no se reintenta
	// hasta pasado el TTL — el cliente recibe 404 (sin Retry-After) y
	// deja de poolear (PB-13).
	if trickplayFailedRecently(itemDir) {
		return "", errTrickplayFailed
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
		// Semáforo global ANTES del deadline: con los slots ocupados
		// la generación espera su turno en cola (el caller HTTP ya
		// recibió pending y poolea) en vez de competir por CPU con
		// otros N ffmpegs y con los transcodes activos (PB-12).
		h.genSlots <- struct{}{}
		defer func() { <-h.genSlots }()
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		params := imaging.TrickplayParams{DurationSeconds: durationSec}
		if _, err := imaging.GenerateTrickplayWithDeadline(bgCtx, itemPath, itemDir, params, 0); err != nil {
			h.logger.Warn("trickplay generation failed (background)",
				"item_id", itemID, "error", err)
			// Marcador de fallo (mtime = timestamp): los próximos
			// requests reciben errTrickplayFailed hasta el TTL en vez
			// de relanzar ffmpeg en bucle.
			if mkErr := os.MkdirAll(itemDir, 0o755); mkErr == nil {
				_ = os.WriteFile(filepath.Join(itemDir, trickplayFailedMarker), nil, 0o644)
			}
			return
		}
		// Generación OK: limpiar cualquier marcador de fallo previo.
		_ = os.Remove(filepath.Join(itemDir, trickplayFailedMarker))
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
