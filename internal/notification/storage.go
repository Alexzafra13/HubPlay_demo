package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// Repository es la persistencia. Patron espejo de federation/storage:
// un wrapper dual-driver sobre los Queries generados por sqlc, que
// expone tipos del paquete `notification` (no de sqlc) hacia arriba.
type Repository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

// NewRepository acepta o un *sql.DB sqlite o un *sql.DB postgres. El
// driver lo discrimina el caller via NewRepository / NewRepositoryPg
// segun la conexion que tenga.
func NewRepository(driver string, db *sql.DB) *Repository {
	r := &Repository{}
	if driver == "postgres" {
		r.pq = sqlc_pg.New(db)
	} else {
		r.sq = sqlc.New(db)
	}
	return r
}

func (r *Repository) useSQLite() bool { return r.sq != nil }

// Insert persiste una notificacion. Genera el UUID si no viene.
func (r *Repository) Insert(ctx context.Context, n *Notification) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	var err error
	if r.useSQLite() {
		err = r.sq.InsertNotification(ctx, sqlc.InsertNotificationParams{
			ID:        n.ID,
			UserID:    n.UserID,
			Kind:      string(n.Kind),
			Title:     n.Title,
			Body:      n.Body,
			Link:      n.Link,
			Payload:   n.Payload,
			CreatedAt: n.CreatedAt,
		})
	} else {
		err = r.pq.InsertNotification(ctx, sqlc_pg.InsertNotificationParams{
			ID:        n.ID,
			UserID:    n.UserID,
			Kind:      string(n.Kind),
			Title:     n.Title,
			Body:      n.Body,
			Link:      n.Link,
			Payload:   n.Payload,
			CreatedAt: n.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// ListForUser devuelve las ultimas `limit` notificaciones del user.
func (r *Repository) ListForUser(ctx context.Context, userID string, limit int) ([]*Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	if r.useSQLite() {
		rows, err := r.sq.ListNotificationsForUser(ctx, sqlc.ListNotificationsForUserParams{
			UserID: userID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list notifications: %w", err)
		}
		out := make([]*Notification, 0, len(rows))
		for i := range rows {
			out = append(out, notificationFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListNotificationsForUser(ctx, sqlc_pg.ListNotificationsForUserParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	out := make([]*Notification, 0, len(rows))
	for i := range rows {
		out = append(out, notificationFromPgRow(rows[i]))
	}
	return out, nil
}

// CountUnreadForUser es la query del badge del header.
func (r *Repository) CountUnreadForUser(ctx context.Context, userID string) (int, error) {
	if r.useSQLite() {
		n, err := r.sq.CountUnreadNotificationsForUser(ctx, userID)
		if err != nil {
			return 0, fmt.Errorf("count unread: %w", err)
		}
		return int(n), nil
	}
	n, err := r.pq.CountUnreadNotificationsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return int(n), nil
}

// MarkRead. Devuelve domain.ErrNotFound si la notif no existe o no
// pertenece al user (el WHERE user_id = ? la oculta). execrows=0 con
// la notif ya leida tambien se considera no-op silencioso.
func (r *Repository) MarkRead(ctx context.Context, notifID, userID string, at time.Time) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.MarkNotificationRead(ctx, sqlc.MarkNotificationReadParams{
			ReadAt: sql.NullTime{Time: at, Valid: true},
			ID:     notifID,
			UserID: userID,
		})
	} else {
		n, err = r.pq.MarkNotificationRead(ctx, sqlc_pg.MarkNotificationReadParams{
			ReadAt: sql.NullTime{Time: at, Valid: true},
			ID:     notifID,
			UserID: userID,
		})
	}
	if err != nil {
		return fmt.Errorf("mark notif read: %w", err)
	}
	if n == 0 {
		// Cubre dos casos: notif inexistente / no le pertenece, y notif
		// ya leida. El handler responde 204 en ambos casos - el cliente
		// no necesita distinguir. Si en algun momento queremos 404 para
		// el primer caso habria que hacer GET previo.
		return errors.Join(domain.ErrNotFound, errors.New("notification not found or already read"))
	}
	return nil
}

// MarkAllReadForUser devuelve cuantas filas se actualizaron para que
// el handler pueda hacer un broadcast del nuevo unread count (=0).
func (r *Repository) MarkAllReadForUser(ctx context.Context, userID string, at time.Time) (int, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.MarkAllNotificationsReadForUser(ctx, sqlc.MarkAllNotificationsReadForUserParams{
			ReadAt: sql.NullTime{Time: at, Valid: true},
			UserID: userID,
		})
	} else {
		n, err = r.pq.MarkAllNotificationsReadForUser(ctx, sqlc_pg.MarkAllNotificationsReadForUserParams{
			ReadAt: sql.NullTime{Time: at, Valid: true},
			UserID: userID,
		})
	}
	if err != nil {
		return 0, fmt.Errorf("mark all read: %w", err)
	}
	return int(n), nil
}

// DeleteOldRead borra notificaciones leidas con read_at < `before`.
// Lo invoca un job periodico (lo wireamos en Phase 5) o se puede
// llamar a mano en testing.
func (r *Repository) DeleteOldRead(ctx context.Context, before time.Time) (int, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteOldReadNotifications(ctx, sql.NullTime{Time: before, Valid: true})
	} else {
		n, err = r.pq.DeleteOldReadNotifications(ctx, sql.NullTime{Time: before, Valid: true})
	}
	if err != nil {
		return 0, fmt.Errorf("delete old notifications: %w", err)
	}
	return int(n), nil
}

func notificationFromSqliteRow(row sqlc.Notification) *Notification {
	n := &Notification{
		ID:        row.ID,
		UserID:    row.UserID,
		Kind:      Kind(row.Kind),
		Title:     row.Title,
		Body:      row.Body,
		Link:      row.Link,
		Payload:   row.Payload,
		CreatedAt: row.CreatedAt,
	}
	if row.ReadAt.Valid {
		t := row.ReadAt.Time
		n.ReadAt = &t
	}
	return n
}

func notificationFromPgRow(row sqlc_pg.Notification) *Notification {
	n := &Notification{
		ID:        row.ID,
		UserID:    row.UserID,
		Kind:      Kind(row.Kind),
		Title:     row.Title,
		Body:      row.Body,
		Link:      row.Link,
		Payload:   row.Payload,
		CreatedAt: row.CreatedAt,
	}
	if row.ReadAt.Valid {
		t := row.ReadAt.Time
		n.ReadAt = &t
	}
	return n
}
