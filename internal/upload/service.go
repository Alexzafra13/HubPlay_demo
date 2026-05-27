package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/probe"
)

// ─── Dependency surfaces ────────────────────────────────────────────
//
// Cada dep está expresada como interface local — los repos / pubsub
// concretos los implementan sin saberlo. Esto permite:
//   1. Tests del pipeline con fakes (sin tocar DB, sin bus real).
//   2. Aislar el upload package del grafo de imports — evita que
//      meterlo en el árbol gire cyclos.

// UserStore es la mínima superficie que el pipeline necesita del
// user repo. Coincide con los métodos ya existentes en
// *db.UserRepository tras la migración 053.
type UserStore interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
	ReserveUploadBytes(ctx context.Context, id string, delta int64) error
	ReleaseUploadBytes(ctx context.Context, id string, delta int64) error
}

// AuditStore es la mínima superficie del repo de auditoría.
type AuditStore interface {
	Insert(ctx context.Context, row db.UploadAuditRow) error
}

// EventPublisher es el lado de "publish" del bus interno. Coincide
// con (*event.Bus).Publish.
type EventPublisher interface {
	Publish(e event.Event)
}

// Prober envuelve ffprobe. Definido como interface para inyectar un
// fake en tests sin spawn de ffmpeg.
type Prober interface {
	Probe(ctx context.Context, path string) (*probe.Result, error)
}

// ─── Config + Service ───────────────────────────────────────────────

// Config gobierna los knobs runtime del módulo.
type Config struct {
	// MaxUploadBytes: tope absoluto por fichero. Por encima → reject
	// en pre-create antes de tocar nada (defense-in-depth sobre la
	// cuota per-user, que es per-usuario y agregada). 0 = sin tope.
	MaxUploadBytes int64

	// MinDurationMs: duración mínima reportada por ffprobe para que
	// un upload de video se acepte. Defensa contra payloads de
	// 1 segundo disfrazados que pasan magic-byte + ffprobe pero no
	// son media real. 1000ms es seguro (un trailer mínimo cumple).
	MinDurationMs int64
}

// DefaultConfig devuelve los valores que el módulo usa cuando el
// operador no especifica nada en YAML.
func DefaultConfig() Config {
	return Config{
		MaxUploadBytes: 50 * 1024 * 1024 * 1024, // 50 GiB
		MinDurationMs:  1000,
	}
}

// Service orquesta el ciclo de vida del upload una vez los bytes ya
// están en staging. El handler tusd llama a HandlePreCreate (para
// validar nombre + reservar cuota) y a HandlePostFinish (para
// disparar el pipeline asíncrono que valida, prueba con ffprobe,
// mueve a librería, audita y publica eventos).
type Service struct {
	cfg     Config
	staging *StagingDir
	users   UserStore
	audit   AuditStore
	bus     EventPublisher
	picker  *LibraryPicker
	prober  Prober
	logger  *slog.Logger
	clock   clock.Clock
}

// NewService cablea las dependencias. Cualquier nil (excepto clk) panics
// — son invariantes de construcción que sólo el bootstrap del binario
// puede romper, y el panic en arranque es preferible al nil-deref
// silencioso a las 3am del primer upload. `clk` opcional — default
// `clock.New()`; inyectable para tests determinísticos.
func NewService(cfg Config, staging *StagingDir, users UserStore, audit AuditStore, bus EventPublisher, picker *LibraryPicker, prober Prober, clk clock.Clock, logger *slog.Logger) *Service {
	if staging == nil || users == nil || audit == nil || bus == nil || picker == nil || prober == nil || logger == nil {
		panic("upload.NewService: nil dependency")
	}
	if clk == nil {
		clk = clock.New()
	}
	return &Service{
		cfg:     cfg,
		staging: staging,
		users:   users,
		audit:   audit,
		bus:     bus,
		picker:  picker,
		prober:  prober,
		logger:  logger.With("module", "upload"),
		clock:   clk,
	}
}

// ─── Pre-create: validation + quota reservation ─────────────────────

// PreCreateInput es lo que el HTTP layer extrae del request tus antes
// de invocar el hook. El campo OriginalName viene de la metadata
// `filename` que el cliente pone; LibraryIDHint del campo `library_id`
// (opcional).
type PreCreateInput struct {
	UserID          string
	UploadID        string
	OriginalName    string
	Size            int64
	LibraryIDHint   string
	// Subpath dentro de la librería destino (PR6 feature file
	// explorer).  Vacío = raíz de la librería. Si viene un valor,
	// SanitizeSubpath lo valida en PreCreate y Finish lo usa para
	// componer el target path.
	Subpath         string
}

// PreCreateResult es lo que el handler tusd usa para responder. Si
// Err != nil, la creación falla y el cliente recibe el código HTTP /
// mensaje pertinente.
type PreCreateResult struct {
	SanitizedName string
	ExtensionOK   bool
}

// PreCreate valida + reserva cuota. Llamarlo desde el
// PreUploadCreateCallback de tusd.
//
// Reglas de fallo (orden cuenta):
//   1. user must exist + be active. Lookups baratos primero.
//   2. size > 0 and <= MaxUploadBytes.
//   3. filename sanitises non-empty.
//   4. extension is in the whitelist.
//   5. quota reserve succeeds (atomic).
//
// La reserva al final es deliberada: las primeras 4 son rejection
// barata; reservar antes y luego revertir por nombre inválido sería
// trabajo a cambio de nada.
func (s *Service) PreCreate(ctx context.Context, in PreCreateInput) (PreCreateResult, error) {
	res := PreCreateResult{}

	user, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		return res, fmt.Errorf("look up user: %w", err)
	}
	if !user.IsActive {
		return res, errors.New("user is not active")
	}
	if !user.CanUpload {
		return res, errors.New("user lacks upload permission")
	}

	if in.Size <= 0 {
		return res, errors.New("upload size must be positive")
	}
	if s.cfg.MaxUploadBytes > 0 && in.Size > s.cfg.MaxUploadBytes {
		return res, fmt.Errorf("upload exceeds server max (%d bytes)", s.cfg.MaxUploadBytes)
	}

	sanitised := SanitizeFilename(in.OriginalName)
	if sanitised == "" {
		return res, errors.New("filename is invalid or empty after sanitisation")
	}
	res.SanitizedName = sanitised

	if err := ValidateExtension(sanitised); err != nil {
		return res, err
	}
	res.ExtensionOK = true

	// Validación del subpath ANTES de reservar cuota — un subpath
	// inválido debe rechazar el upload entero sin gastar slots de
	// cuota. La forma canónica se guardará junto con la metadata
	// tusd y Finish la reusará tal cual.
	if _, err := SanitizeSubpath(in.Subpath); err != nil {
		return res, err
	}

	// Quota reserve: atómica en el repo, devuelve ErrUploadQuotaExceeded
	// si excede o si can_upload fue revocado entre el lookup de arriba
	// y este punto (race-safe).
	if err := s.users.ReserveUploadBytes(ctx, in.UserID, in.Size); err != nil {
		return res, fmt.Errorf("reserve upload bytes: %w", err)
	}

	s.logger.Info("upload pre-create accepted",
		"user_id", in.UserID,
		"upload_id", in.UploadID,
		"sanitized_name", sanitised,
		"size", in.Size)
	return res, nil
}

// PreCreateRollback libera bytes reservados cuando algo entre el
// PreCreate y la primera escritura falla (p.ej. el storage de tusd
// rechaza). Idempotente — llamarla dos veces sólo decrementa una.
func (s *Service) PreCreateRollback(ctx context.Context, userID string, bytes int64) {
	if err := s.users.ReleaseUploadBytes(ctx, userID, bytes); err != nil {
		s.logger.Warn("pre-create rollback failed",
			"user_id", userID, "bytes", bytes, "error", err)
	}
}

// ─── Pipeline: validación binaria → ffprobe → move → audit ──────────

// FinishInput es lo que tusd entrega tras el último chunk. SourcePath
// apunta al blob ya completo en el staging; OriginalName / SanitizedName
// vienen de PreCreate (tusd los guarda en su FileInfo.MetaData).
type FinishInput struct {
	UserID         string
	UploadID       string
	OriginalName   string
	SanitizedName  string
	LibraryIDHint  string
	// Subpath dentro de la librería destino. Vacío = raíz.
	Subpath        string
	// Overwrite: si true y el fichero destino existe, lo pisa en vez
	// de añadir sufijo -NNN. Lo decide el cliente vía metadata tras
	// preguntar al usuario en un modal de colisión. Default false
	// (sufijo) — sin esta opción explícita NUNCA se pisa, evita
	// pérdidas de datos por race u olvido del modal.
	Overwrite      bool
	Size           int64
	SourcePath     string
	StartedAt      time.Time
}

// FinishResult es lo que la goroutine del pipeline produce. El audit
// row ya ha sido insertado; el caller no tiene que volver a auditar.
type FinishResult struct {
	Outcome      string // accepted | rejected | error
	LibraryID    string
	FinalPath    string // relativo al data dir (vacío si no aterrizó)
	MimeDetected string
	SHA256       string
	DurationMs   int64
	ErrorMessage string
}

// Finish corre el pipeline post-upload de forma SÍNCRONA. El HTTP
// handler lo ejecuta en una goroutine (spawn-and-forget desde
// PostFinish) para no bloquear la respuesta tus.
//
// Fases (cada una publica un evento UploadPhase + UploadDone/Error
// al final):
//   1. validating: magic-byte detection sobre los primeros SniffLength bytes.
//   2. probing   : ffprobe sobre el fichero completo. Rechaza < MinDurationMs.
//   3. moving    : compone targetPath = <library_path>/<sanitizedName> con
//                  sufijo numérico ante colisión; MoveTo atómico.
//   4. indexing  : publica ItemAdded en el bus para que el scanner indexe
//                  sin esperar al próximo barrido programado.
//
// Cualquier fallo cierra con UploadError, libera cuota, escribe audit
// outcome=rejected/error, devuelve. La fase anterior al fallo queda
// loggeada en error_message para diagnóstico.
func (s *Service) Finish(ctx context.Context, in FinishInput) FinishResult {
	started := in.StartedAt
	if started.IsZero() {
		started = s.clock.Now().UTC()
	}
	res := FinishResult{Outcome: "error", ErrorMessage: "pipeline did not run to completion"}

	finalize := func(outcome, errMsg, libraryID, finalPath string) FinishResult {
		res.Outcome = outcome
		res.ErrorMessage = errMsg
		res.LibraryID = libraryID
		res.FinalPath = finalPath
		now := s.clock.Now().UTC()
		_ = s.audit.Insert(ctx, db.UploadAuditRow{
			ID:           RandomID(),
			UserID:       in.UserID,
			LibraryID:    libraryID,
			OriginalName: in.OriginalName,
			FinalPath:    finalPath,
			Bytes:        in.Size,
			SHA256:       res.SHA256,
			MimeDetected: res.MimeDetected,
			Outcome:      outcome,
			ErrorMessage: errMsg,
			StartedAt:    started,
			FinishedAt:   now,
			DurationMs:   now.Sub(started).Milliseconds(),
		})
		if outcome == "accepted" {
			s.publish(event.UploadDone, map[string]any{
				"id":         in.UploadID,
				"user_id":    in.UserID,
				"library_id": libraryID,
				"final_path": finalPath,
			})
		} else {
			// En cualquier fallo devolvemos los bytes a la cuota.
			s.users.ReleaseUploadBytes(ctx, in.UserID, in.Size) //nolint:errcheck
			s.publish(event.UploadError, map[string]any{
				"id":      in.UploadID,
				"user_id": in.UserID,
				"reason":  errMsg,
			})
		}
		return res
	}

	// ── Phase 1: validating (magic bytes + extension recheck) ───────
	s.publish(event.UploadPhase, map[string]any{"id": in.UploadID, "user_id": in.UserID, "phase": "validating"})
	if err := ValidateExtension(in.SanitizedName); err != nil {
		return finalize("rejected", err.Error(), "", "")
	}
	kind, mime, err := s.sniffKind(in.SourcePath)
	if err != nil {
		return finalize("rejected", "sniff: "+err.Error(), "", "")
	}
	res.MimeDetected = mime

	// ── Phase 2: probing (ffprobe) ──────────────────────────────────
	// Subtítulos saltan ffprobe — no son media decodificable.
	if kind == KindVideo {
		s.publish(event.UploadPhase, map[string]any{"id": in.UploadID, "user_id": in.UserID, "phase": "probing"})
		result, err := s.prober.Probe(ctx, in.SourcePath)
		if err != nil {
			return finalize("rejected", "ffprobe: "+err.Error(), "", "")
		}
		durMs := result.Format.Duration.Milliseconds()
		res.DurationMs = durMs
		if durMs < s.cfg.MinDurationMs {
			return finalize("rejected",
				fmt.Sprintf("duration %dms below min %dms", durMs, s.cfg.MinDurationMs),
				"", "")
		}
	}

	// ── Phase 2.5: hash (cheap with already-on-disk file) ───────────
	if h, err := hashFile(in.SourcePath); err == nil {
		res.SHA256 = h
	} else {
		// El hash es nice-to-have para auditoría; no abortamos si falla.
		s.logger.Warn("sha256 failed", "upload_id", in.UploadID, "error", err)
	}

	// ── Phase 3: moving ─────────────────────────────────────────────
	s.publish(event.UploadPhase, map[string]any{"id": in.UploadID, "user_id": in.UserID, "phase": "moving"})
	lib, err := s.picker.PickDestination(ctx, in.UserID, in.LibraryIDHint, kind)
	if err != nil {
		return finalize("rejected", "pick library: "+err.Error(), "", "")
	}
	// Subpath: si el cliente especificó una carpeta dentro de la
	// librería (vía el file explorer), aterrizamos ahí. Sin subpath,
	// va a la raíz (comportamiento pre-PR6).  ResolveSubpath crea la
	// ruta absoluta + verifica que vive dentro de la librería; si
	// hubiera traversal lo rechazamos como error de pipeline (no
	// "rejected" — los chequeos eran cliente-side y PreCreate, llegar
	// aquí con subpath inválido sería bug del propio backend).
	targetDir, err := ResolveSubpath(lib.Paths[0], in.Subpath)
	if err != nil {
		return finalize("error", "resolve subpath: "+err.Error(), lib.ID, "")
	}
	// Aseguramos que el sub-dir destino existe — primer upload a una
	// carpeta nueva creada via "New folder" del cliente cae aquí si
	// el endpoint POST /folders no lo creó antes.
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return finalize("error", "create target dir: "+err.Error(), lib.ID, "")
	}
	// Path destino del fichero. Dos modos:
	//   1) Overwrite=true: el cliente ya preguntó al usuario, pisamos
	//      el fichero existente (borra antes para que MoveTo no
	//      vea colisión y mantengamos la semántica atómica del rename).
	//   2) Overwrite=false (default): resolveTargetPath añade sufijo
	//      -NNN ante colisión — comportamiento histórico que protege
	//      contra subir dos veces el mismo fichero sin querer.
	var target string
	if in.Overwrite {
		target = filepath.Join(targetDir, in.SanitizedName)
		// Borra el destino si existe — IGNORANDO error si no estaba.
		// MoveTo rechaza con ErrTargetExists; el overwrite REQUIERE
		// que esa precondición no se dispare.
		if _, err := os.Stat(target); err == nil {
			if err := os.Remove(target); err != nil {
				return finalize("error", "remove for overwrite: "+err.Error(), lib.ID, "")
			}
		}
	} else {
		t, err := s.resolveTargetPath(targetDir, in.SanitizedName)
		if err != nil {
			return finalize("error", "resolve target: "+err.Error(), lib.ID, "")
		}
		target = t
	}
	if err := s.staging.MoveTo(in.SourcePath, target); err != nil {
		return finalize("error", "move: "+err.Error(), lib.ID, "")
	}

	// ── Phase 4: indexing — fire-and-forget event so the scanner ────
	// picks it up on its next pass / immediately if it's listening.
	s.publish(event.UploadPhase, map[string]any{"id": in.UploadID, "user_id": in.UserID, "phase": "indexing"})
	s.publish(event.ItemAdded, map[string]any{
		"library_id": lib.ID,
		"path":       target,
		"source":     "upload",
	})

	// Cleanup del staging-dir-del-upload (puede tener fichero `.info`
	// de tusd y los chunks intermedios). Best-effort.
	_ = s.staging.RemoveUpload(in.UserID, in.UploadID)

	return finalize("accepted", "", lib.ID, target)
}

// Aborted llama desde el handler cuando el cliente cancela un upload
// en curso (DELETE en tus). Libera la cuota reservada y escribe la
// audit row con outcome=aborted. Best-effort: si algo falla, loguea
// y sigue — no hay nadie esperando una respuesta útil del cliente
// que ya colgó.
func (s *Service) Aborted(ctx context.Context, in FinishInput) {
	started := in.StartedAt
	if started.IsZero() {
		started = s.clock.Now().UTC()
	}
	now := s.clock.Now().UTC()
	_ = s.audit.Insert(ctx, db.UploadAuditRow{
		ID:           RandomID(),
		UserID:       in.UserID,
		OriginalName: in.OriginalName,
		Bytes:        in.Size,
		Outcome:      "aborted",
		ErrorMessage: "client cancelled or disconnected",
		StartedAt:    started,
		FinishedAt:   now,
		DurationMs:   now.Sub(started).Milliseconds(),
	})
	if err := s.users.ReleaseUploadBytes(ctx, in.UserID, in.Size); err != nil {
		s.logger.Warn("abort release failed",
			"user_id", in.UserID, "bytes", in.Size, "error", err)
	}
	_ = s.staging.RemoveUpload(in.UserID, in.UploadID)
	s.publish(event.UploadError, map[string]any{
		"id":      in.UploadID,
		"user_id": in.UserID,
		"reason":  "aborted",
	})
}

// ─── helpers ────────────────────────────────────────────────────────

func (s *Service) publish(t event.Type, data map[string]any) {
	s.bus.Publish(event.Event{Type: t, Data: data})
}

// sniffKind abre el fichero, lee los primeros SniffLength bytes y
// los pasa al validator. Cerramos el fichero antes de retornar para
// que el ffprobe siguiente lo abra limpiamente (en Windows un handle
// abierto bloquearía).
func (s *Service) sniffKind(path string) (MediaKind, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return KindUnknown, "", fmt.Errorf("open: %w", err)
	}
	defer f.Close() //nolint:errcheck
	return DetectKind(io.LimitReader(f, SniffLength))
}

// resolveTargetPath compone `<libraryPath>/<name>` y, ante colisión
// de nombre, añade un sufijo `-NNN` antes de la extensión hasta
// encontrar un slot libre. Cap a 1000 intentos — más es señal de que
// algo más está pasando (filesystem podrido, race con otro upload).
func (s *Service) resolveTargetPath(libraryPath, name string) (string, error) {
	abs, err := filepath.Abs(libraryPath)
	if err != nil {
		return "", fmt.Errorf("abs library path: %w", err)
	}
	candidate := filepath.Join(abs, name)
	if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	}
	// Colisión: añadir sufijo. Mantenemos extension.
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate = filepath.Join(abs, stem+"-"+strconv.Itoa(i)+ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find free slot for %q after 1000 attempts", name)
}

// hashFile calcula el SHA-256 hex del fichero. Streaming, no carga
// nada en memoria — 32 KiB buffer del default de io.Copy es ok
// porque ya hicimos el move; aquí estamos en disco caliente con el
// fichero recién escrito.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
