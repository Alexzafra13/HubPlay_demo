// Package audit centraliza la emisión de eventos al audit_log
// unificado (PR5). Sustituye los logs en texto + audit fragmentado
// (upload_audit sigue existiendo para su shape específico — este
// service también puede registrar uploads via LogUploadOutcome para
// que aparezcan en el panel unificado).
//
// El diseño expone métodos NAMED por tipo de evento — `LogAuthLogin`,
// `LogPermissionChanged` — en vez de un genérico `Log(eventType,
// payload)`. Las ventajas:
//   1. El call site se autodescribe (busca "LogPermissionChanged" en
//      grep y encuentras todos los productores).
//   2. El payload typed por evento — el método valida shape antes de
//      llegar al JSON. Sin riesgo de drift entre productores.
//   3. Cualquier productor que falla porque el campo cambió de nombre
//      revienta en compile, no en runtime.
//
// El servicio es FIRE-AND-FORGET: cada método loguea el error si el
// INSERT falla pero no devuelve nada al caller. Auditar es secundario
// — no queremos que un INSERT lento (disco saturado, lock) bloquee
// el flujo principal (login, upload, etc.).  El precio: en una caída
// catastrófica de la DB podríamos perder algunos eventos del último
// segundo. Aceptable para audit ligero; sistemas de auditoría duros
// (HIPAA, finance) necesitarían two-phase commit con el evento.
package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/auth"
)

// LogRow es la representación que el service emite al store. Es un
// struct espejo propio del paquete (cierre del olor SS-2): antes la
// interface tomaba db.AuditLogRow, lo que obligaba a importar
// internal/db sólo para construir el parámetro. Con el tipo propio el
// paquete audit ya no depende de la capa de persistencia (DIP) — el
// adapter del composition root convierte LogRow → db.AuditLogRow en la
// frontera. Sólo lleva los campos que el INSERT persiste (actor/target
// username se resuelven en read vía join).
type LogRow struct {
	ID          string
	ActorUserID string
	EventType   string
	TargetType  string
	TargetID    string
	Payload     string // JSON o cadena vacía
	IPAddress   string
	UserAgent   string
	CreatedAt   time.Time
}

// Store es la mínima superficie del repo que el service necesita.
// Definida como interface para que tests pasen un fake sin DB.
type Store interface {
	Insert(ctx context.Context, row LogRow) error
}

type Service struct {
	store  Store
	logger *slog.Logger
}

func NewService(store Store, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger.With("module", "audit")}
}

// requestMeta extrae IP + UA del request HTTP. Centralizado aquí
// para que cada productor no replique el parsing (X-Forwarded-For
// vs RemoteAddr, etc.). Si r es nil, devuelve vacíos — eventos
// generados por jobs background no tienen request.
func requestMeta(r *http.Request) (ip, ua string) {
	if r == nil {
		return "", ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Primer hop del XFF — el cliente original. Trusted proxies
		// los configura el operador (config.RateLimit.TrustedSubnets).
		// Para audit basta con el primer valor.
		if idx := indexByte(xff, ','); idx > 0 {
			ip = xff[:idx]
		} else {
			ip = xff
		}
	} else {
		ip = r.RemoteAddr
	}
	ua = r.UserAgent()
	if len(ua) > 256 {
		// User agents largos son pseudo-DoS sobre el audit log. Truncamos.
		ua = ua[:256]
	}
	return ip, ua
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// actorFromContext extrae el actor user id de las claims JWT. Si la
// request no tiene claims (login fallido, llamada anónima), devuelve
// cadena vacía — la fila queda con actor_user_id NULL.
func actorFromContext(ctx context.Context) string {
	if c := auth.GetClaims(ctx); c != nil {
		return c.UserID
	}
	return ""
}

// randomID genera un id hex de 16 bytes (32 chars). Mismo patrón que
// upload.RandomID — colisión despreciable a la escala del audit log.
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback determinístico para no panicar — el operador verá
		// el id raro en logs si llega.
		return "rand-fallback-" + time.Now().UTC().Format("20060102150405.000000")
	}
	return hex.EncodeToString(b[:])
}

// emit es el helper interno común. Cada Log* público lo invoca tras
// componer el payload.
func (s *Service) emit(ctx context.Context, r *http.Request, eventType, targetType, targetID, actorOverride string, payload any) {
	if s == nil {
		return // tests / configuraciones que no cablean el service.
	}

	ip, ua := requestMeta(r)
	actor := actorOverride
	if actor == "" {
		actor = actorFromContext(ctx)
	}

	var payloadJSON string
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		} else {
			s.logger.Warn("audit: payload marshal failed",
				"event", eventType, "error", err)
		}
	}

	row := LogRow{
		ID:          randomID(),
		ActorUserID: actor,
		EventType:   eventType,
		TargetType:  targetType,
		TargetID:    targetID,
		Payload:     payloadJSON,
		IPAddress:   ip,
		UserAgent:   ua,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.Insert(ctx, row); err != nil {
		// Fire-and-forget — el error queda en logs, no bloquea el
		// flujo principal del caller.
		s.logger.Warn("audit insert failed",
			"event", eventType, "error", err)
	}
}

// ─── Auth events ────────────────────────────────────────────────────

// LogAuthLogin: éxito de login. actorUserID es el id del user que
// acaba de autenticarse (no viene en claims aún en el momento del
// login, así que el caller lo pasa explícito).
func (s *Service) LogAuthLogin(ctx context.Context, r *http.Request, actorUserID, username string) {
	s.emit(ctx, r, "auth.login.ok", "user", actorUserID, actorUserID, map[string]any{
		"username": username,
	})
}

// LogAuthLoginFailed: contraseña/usuario incorrectos. Actor vacío
// (no auth aún); username lo pasa el handler para que el operador
// pueda detectar intentos contra una cuenta concreta.
func (s *Service) LogAuthLoginFailed(ctx context.Context, r *http.Request, attemptedUsername, reason string) {
	s.emit(ctx, r, "auth.login.failed", "", "", "", map[string]any{
		"username": attemptedUsername,
		"reason":   reason,
	})
}

// LogAuthLogout: el usuario hace logout explícito (o se le revoca
// la sesión).
func (s *Service) LogAuthLogout(ctx context.Context, r *http.Request, actorUserID, sessionID string) {
	s.emit(ctx, r, "auth.logout", "session", sessionID, actorUserID, nil)
}

// LogSessionRevoked: un admin / el propio user revoca una sesión
// activa (panel /me/sessions o /admin).
func (s *Service) LogSessionRevoked(ctx context.Context, r *http.Request, sessionID, ownerUserID string) {
	s.emit(ctx, r, "auth.session.revoked", "session", sessionID, "", map[string]any{
		"session_owner": ownerUserID,
	})
}

// ─── Permission / user management ───────────────────────────────────

// LogPermissionChanged: un PUT /users/{id}/permissions cambió uno o
// varios flags. Payload incluye los flags modificados con su valor
// nuevo. NO incluye los flags que no cambiaron (sería ruido).
func (s *Service) LogPermissionChanged(ctx context.Context, r *http.Request, targetUserID string, changes map[string]bool) {
	s.emit(ctx, r, "permission.changed", "user", targetUserID, "", map[string]any{
		"changes": changes,
	})
}

// LogRoleChanged: PUT /users/{id}/role promovió/degradó.
func (s *Service) LogRoleChanged(ctx context.Context, r *http.Request, targetUserID, oldRole, newRole string) {
	s.emit(ctx, r, "permission.role_changed", "user", targetUserID, "", map[string]any{
		"old": oldRole, "new": newRole,
	})
}

// LogUserCreated: alta nueva.
func (s *Service) LogUserCreated(ctx context.Context, r *http.Request, newUserID, username, role string) {
	s.emit(ctx, r, "user.created", "user", newUserID, "", map[string]any{
		"username": username, "role": role,
	})
}

// LogUserDeleted: baja.
func (s *Service) LogUserDeleted(ctx context.Context, r *http.Request, deletedUserID, deletedUsername string) {
	s.emit(ctx, r, "user.deleted", "user", deletedUserID, "", map[string]any{
		"username": deletedUsername,
	})
}

// LogUserActiveChanged: usuario activado/desactivado.
func (s *Service) LogUserActiveChanged(ctx context.Context, r *http.Request, targetUserID string, active bool) {
	s.emit(ctx, r, "user.active_changed", "user", targetUserID, "", map[string]any{
		"active": active,
	})
}

// LogPasswordReset: admin resetea contraseña de otro user. NO incluye
// la nueva contraseña en el payload — el caller que la conoce ya la
// muestra al admin en la UI; el audit es trazabilidad, no recuperación.
func (s *Service) LogPasswordReset(ctx context.Context, r *http.Request, targetUserID string) {
	s.emit(ctx, r, "user.password_reset", "user", targetUserID, "", nil)
}

// ─── Library / catalog ──────────────────────────────────────────────

// LogLibraryCreated: alta de librería.
func (s *Service) LogLibraryCreated(ctx context.Context, r *http.Request, libraryID, name, contentType string) {
	s.emit(ctx, r, "library.created", "library", libraryID, "", map[string]any{
		"name": name, "content_type": contentType,
	})
}

// LogLibraryDeleted: baja.
func (s *Service) LogLibraryDeleted(ctx context.Context, r *http.Request, libraryID, name string) {
	s.emit(ctx, r, "library.deleted", "library", libraryID, "", map[string]any{
		"name": name,
	})
}

// LogLibraryScanStarted: scan manual disparado por el admin (no los
// automáticos del scheduler — esos son periódicos y no necesitan
// audit individual).
func (s *Service) LogLibraryScanStarted(ctx context.Context, r *http.Request, libraryID string) {
	s.emit(ctx, r, "library.scan_started", "library", libraryID, "", nil)
}

// LogMetadataEdited: edición manual o identify aplicado sobre un item.
// kind = "manual" | "identify_tmdb" | "refresh".
func (s *Service) LogMetadataEdited(ctx context.Context, r *http.Request, itemID, kind string) {
	s.emit(ctx, r, "metadata.edited", "item", itemID, "", map[string]any{
		"kind": kind,
	})
}

// LogArtworkChanged: poster/backdrop/logo cambiado.
// targetType = "item" o "collection"; kind = "poster" | "backdrop" | "logo".
func (s *Service) LogArtworkChanged(ctx context.Context, r *http.Request, targetType, targetID, kind string) {
	s.emit(ctx, r, "artwork.changed", targetType, targetID, "", map[string]any{
		"kind": kind,
	})
}

// ─── IPTV ───────────────────────────────────────────────────────────

func (s *Service) LogIPTVImported(ctx context.Context, r *http.Request, libraryID string, channelCount int) {
	s.emit(ctx, r, "iptv.m3u_imported", "library", libraryID, "", map[string]any{
		"channels": channelCount,
	})
}

func (s *Service) LogChannelDisabled(ctx context.Context, r *http.Request, channelID string) {
	s.emit(ctx, r, "iptv.channel_disabled", "channel", channelID, "", nil)
}

func (s *Service) LogChannelEnabled(ctx context.Context, r *http.Request, channelID string) {
	s.emit(ctx, r, "iptv.channel_enabled", "channel", channelID, "", nil)
}

// ─── CORS ───────────────────────────────────────────────────────────

func (s *Service) LogCorsOriginAdded(ctx context.Context, r *http.Request, origin, note string) {
	s.emit(ctx, r, "cors.origin_added", "cors_origin", origin, "", map[string]any{
		"note": note,
	})
}

func (s *Service) LogCorsOriginRemoved(ctx context.Context, r *http.Request, origin string) {
	s.emit(ctx, r, "cors.origin_removed", "cors_origin", origin, "", nil)
}

// ─── System ─────────────────────────────────────────────────────────

func (s *Service) LogBackupDownloaded(ctx context.Context, r *http.Request) {
	s.emit(ctx, r, "system.backup_downloaded", "", "", "", nil)
}

func (s *Service) LogBackupRestored(ctx context.Context, r *http.Request) {
	s.emit(ctx, r, "system.backup_restored", "", "", "", nil)
}

func (s *Service) LogSystemRestart(ctx context.Context, r *http.Request, reason string) {
	s.emit(ctx, r, "system.restart", "", "", "", map[string]any{
		"reason": reason,
	})
}

func (s *Service) LogDBSwap(ctx context.Context, r *http.Request, oldDriver, newDriver string) {
	s.emit(ctx, r, "system.db_swap", "", "", "", map[string]any{
		"old": oldDriver, "new": newDriver,
	})
}

// ─── Upload (sync con upload_audit existente) ───────────────────────

// LogUploadOutcome se llama desde el service de upload al final de la
// pipeline. Duplicar la info en audit_log permite que el panel
// unificado los muestre junto con el resto de eventos en lugar de
// que el operador tenga que ir a una vista separada.
func (s *Service) LogUploadOutcome(ctx context.Context, userID, originalName, libraryID, outcome, finalPath string, bytes int64) {
	s.emit(ctx, nil, "upload."+outcome, "library", libraryID, userID, map[string]any{
		"original_name": originalName,
		"final_path":    finalPath,
		"bytes":         bytes,
	})
}
