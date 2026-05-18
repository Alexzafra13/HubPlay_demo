// Package notification implementa un inbox de notificaciones generico
// por usuario, persistido en la tabla `notifications` (migration 049).
//
// Cualquier feature puede emitir entradas via Service.Create(...) - el
// discriminador es el `kind` (string libre) y el `payload` JSON, asi
// que añadir tipos nuevos no toca el schema. El frontend mapea `kind`
// a icono + texto + link.
//
// Persistencia + push:
//   - Cada Create() escribe una fila en DB.
//   - Si hay un EventBus inyectado, publica tambien EventCreated con
//     {user_id, notification_id, kind} para que el SSE de /me/events
//     despierte al cliente del usuario destino sin esperar al
//     siguiente refetch de TanStack.
package notification

import "time"

// Kind discrimina el tipo de notificacion. Convencion: "<feature>.<event>".
// El frontend usa esto para elegir icono + texto traducido + el link
// al que llevar al hacer click.
type Kind string

const (
	// KindPairingRequestReceived: un servidor remoto nos ha enviado una
	// peticion de emparejamiento. Solo se emite a admins. Click lleva
	// a /admin/peers (tab "Peticiones recibidas").
	KindPairingRequestReceived Kind = "federation.pairing_request_received"

	// KindPairingRequestAccepted: el remoto al que enviamos una peticion
	// la acepto. Solo se emite al admin que la inicio. Click lleva a
	// /admin/peers (tab "Servidores emparejados").
	KindPairingRequestAccepted Kind = "federation.pairing_request_accepted"

	// KindPairingRequestDeclined: el remoto la rechazo. Click lleva a
	// /admin/peers (tab "Peticiones enviadas") para que el admin sepa
	// la causa o reintente.
	KindPairingRequestDeclined Kind = "federation.pairing_request_declined"
)

// Notification es una entrada del inbox del usuario.
//
// `Payload` es JSON serializado (string en DB). Cada Kind define su
// propia shape; el frontend hace el parse selectivo segun el Kind.
// Vacio si la notification no necesita datos extra mas alla de title
// + body + link.
type Notification struct {
	ID        string
	UserID    string
	Kind      Kind
	Title     string
	Body      string
	Link      string
	Payload   string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// IsRead devuelve si el usuario ya marco la notificacion como leida.
func (n *Notification) IsRead() bool {
	return n.ReadAt != nil
}
