package main

import (
	"context"
	"log/slog"

	"hubplay/internal/event"
	"hubplay/internal/federation"
	"hubplay/internal/notification"
)

// registerFederationNotifications subscribe el notification service
// a los eventos relevantes del federation manager y traduce cada uno
// en entradas concretas del inbox de notificaciones de los admins.
//
// El acoplamiento va de notification hacia event (notification depende
// del bus, no de federation). Federation solo publica eventos del bus
// estandar y NO sabe nada de notifications. El wire-up vive aqui en
// la composition root para que ambos paquetes sigan independientes.
//
// La suscripcion al bus devuelve un closure de unsub que se ejecuta
// cuando el ctx se cancela (graceful shutdown). Sin esto el handler
// queda colgado y los publishes del bus seguirian llamandolo cuando
// el resto del proceso ya no existe.
func registerFederationNotifications(ctx context.Context, bus *event.Bus, notifs *notification.Service, logger *slog.Logger) {
	if bus == nil || notifs == nil {
		return
	}

	type payload struct {
		RequestID   string `json:"request_id,omitempty"`
		PeerName    string `json:"peer_name,omitempty"`
		ServerUUID  string `json:"server_uuid,omitempty"`
		Fingerprint string `json:"fingerprint,omitempty"`
		PeerID      string `json:"peer_id,omitempty"`
	}

	stringFromData := func(e event.Event, key string) string {
		if e.Data == nil {
			return ""
		}
		v, _ := e.Data[key].(string)
		return v
	}

	// federation.pairing_request_received -> notif a TODOS los admins.
	unsubReceived := bus.Subscribe(federation.EventPairingRequestReceived, func(e event.Event) {
		p := payload{
			RequestID:   stringFromData(e, "request_id"),
			PeerName:    stringFromData(e, "peer_name"),
			ServerUUID:  stringFromData(e, "server_uuid"),
			Fingerprint: stringFromData(e, "fingerprint"),
		}
		title := "Nueva peticion de emparejamiento"
		body := p.PeerName + " quiere emparejarse contigo. Compara la huella antes de aceptar."
		if p.PeerName == "" {
			body = "Un servidor remoto quiere emparejarse contigo. Compara la huella antes de aceptar."
		}
		if _, err := notifs.FanOutToAdmins(ctx, notification.KindPairingRequestReceived,
			title, body, "/admin/peers", p); err != nil {
			logger.Warn("notifications: fan-out pairing received failed", "err", err)
		}
	})

	// federation.pairing_request_accepted -> notif solo al admin local
	// que envio la peticion. El responded_by_user_id viene en payload?
	// En el bus actual no — usamos FanOutToAdmins como fallback (todos
	// los admins se enteran del nuevo peer, sigue siendo informacion
	// utill aunque no fueran quien la inicio).
	unsubAccepted := bus.Subscribe(federation.EventPairingRequestAccepted, func(e event.Event) {
		p := payload{
			RequestID:  stringFromData(e, "request_id"),
			PeerName:   stringFromData(e, "peer_name"),
			ServerUUID: stringFromData(e, "server_uuid"),
			PeerID:     stringFromData(e, "peer_id"),
		}
		title := "Peticion aceptada"
		body := p.PeerName + " acepto tu peticion. Ya esta emparejado y puedes compartir bibliotecas."
		if _, err := notifs.FanOutToAdmins(ctx, notification.KindPairingRequestAccepted,
			title, body, "/admin/peers", p); err != nil {
			logger.Warn("notifications: fan-out pairing accepted failed", "err", err)
		}
	})

	unsubDeclined := bus.Subscribe(federation.EventPairingRequestDeclined, func(e event.Event) {
		p := payload{
			RequestID: stringFromData(e, "request_id"),
			PeerName:  stringFromData(e, "peer_name"),
		}
		title := "Peticion rechazada"
		body := p.PeerName + " rechazo la peticion de emparejamiento."
		if _, err := notifs.FanOutToAdmins(ctx, notification.KindPairingRequestDeclined,
			title, body, "/admin/peers", p); err != nil {
			logger.Warn("notifications: fan-out pairing declined failed", "err", err)
		}
	})

	// Cuando el contexto se cancela (graceful shutdown) limpiamos los
	// suscriptores para no dejar handlers colgados en el bus.
	go func() {
		<-ctx.Done()
		unsubReceived()
		unsubAccepted()
		unsubDeclined()
	}()
}
