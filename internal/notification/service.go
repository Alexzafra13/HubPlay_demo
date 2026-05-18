package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/event"
)

// EventCreated se publica al EventBus tras cada Create exitoso.
// El handler de /me/events lo filtra por user_id y lo empuja al
// frontend, que invalida la query de notifications + refresca el
// contador del badge sin esperar al siguiente polling.
const EventCreated event.Type = "notification.created"

// AdminLister es la slice del UserRepository que el service necesita
// para hacer fan-out de notificaciones admin-target (e.g. "ha entrado
// una pairing request" se notifica a TODOS los admins). Declararla
// aqui (en vez de importar internal/db) mantiene este paquete leaf
// del grafo - sin esto un test rig tendria que arrastrar todo el
// repo de users.
type AdminLister interface {
	ListAdminIDs(ctx context.Context) ([]string, error)
}

// EventPublisher es la slice del event bus que usamos para emitir
// EventCreated. Igual que AdminLister: interface estrecha local
// para no atar este paquete al bus concreto.
type EventPublisher interface {
	Publish(event.Event)
}

// Service es la API publica del paquete. Toda creacion / consulta /
// marcado pasa por aqui; los handlers HTTP son thin wrappers.
type Service struct {
	repo   *Repository
	admins AdminLister
	bus    EventPublisher
	clock  clock.Clock
	logger *slog.Logger
}

// NewService — bus y admins son opcionales (nil = features
// desactivadas: sin bus no hay push SSE, sin admins el fan-out
// FanOutToAdmins es no-op). El logger es required.
func NewService(repo *Repository, admins AdminLister, bus EventPublisher, clk clock.Clock, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if clk == nil {
		clk = clock.New()
	}
	return &Service{
		repo:   repo,
		admins: admins,
		bus:    bus,
		clock:  clk,
		logger: logger.With("module", "notification"),
	}
}

// Create persiste una notificacion para `userID` y publica el
// evento. `payload` puede ser nil; si no, lo serializa a JSON.
func (s *Service) Create(ctx context.Context, userID string, kind Kind, title, body, link string, payload any) (*Notification, error) {
	if userID == "" {
		return nil, fmt.Errorf("notification: user_id required")
	}
	if title == "" {
		return nil, fmt.Errorf("notification: title required")
	}
	var payloadJSON string
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("notification: marshal payload: %w", err)
		}
		payloadJSON = string(raw)
	}
	n := &Notification{
		UserID:    userID,
		Kind:      kind,
		Title:     title,
		Body:      body,
		Link:      link,
		Payload:   payloadJSON,
		CreatedAt: s.clock.Now().UTC(),
	}
	if err := s.repo.Insert(ctx, n); err != nil {
		return nil, err
	}
	s.publish(n)
	return n, nil
}

// FanOutToAdmins crea la misma notificacion para todos los admins
// activos del install. Usado por features que tienen un destinatario
// "admin generico" (pairing requests entrantes, alertas de sistema).
// Si un admin tiene fallo de insert se loguea pero se siguen los
// demas — best-effort.
func (s *Service) FanOutToAdmins(ctx context.Context, kind Kind, title, body, link string, payload any) (int, error) {
	if s.admins == nil {
		return 0, nil
	}
	ids, err := s.admins.ListAdminIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("notification: list admins: %w", err)
	}
	created := 0
	for _, id := range ids {
		if _, err := s.Create(ctx, id, kind, title, body, link, payload); err != nil {
			s.logger.Warn("notification fan-out: insert failed",
				"user_id", id, "kind", kind, "err", err)
			continue
		}
		created++
	}
	return created, nil
}

// ListForUser pasa la query directa al repo. Limit por defecto = 50
// si <= 0 (el repo lo aplica).
func (s *Service) ListForUser(ctx context.Context, userID string, limit int) ([]*Notification, error) {
	return s.repo.ListForUser(ctx, userID, limit)
}

// UnreadCountForUser tipica el badge.
func (s *Service) UnreadCountForUser(ctx context.Context, userID string) (int, error) {
	return s.repo.CountUnreadForUser(ctx, userID)
}

// MarkRead — el handler guard ya valido que el caller es el dueño;
// el repo aplica un WHERE user_id extra como defense-in-depth.
func (s *Service) MarkRead(ctx context.Context, notifID, userID string) error {
	return s.repo.MarkRead(ctx, notifID, userID, s.clock.Now().UTC())
}

func (s *Service) MarkAllReadForUser(ctx context.Context, userID string) (int, error) {
	return s.repo.MarkAllReadForUser(ctx, userID, s.clock.Now().UTC())
}

// publish envuelve la emision del evento. El user_id viaja en Data
// para que el SSE de /me/events filtre.
func (s *Service) publish(n *Notification) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(event.Event{
		Type: EventCreated,
		Data: map[string]any{
			"user_id":         n.UserID,
			"notification_id": n.ID,
			"kind":            string(n.Kind),
			"created_at":      n.CreatedAt.Format(time.RFC3339),
		},
	})
}
