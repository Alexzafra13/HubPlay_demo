package api

import (
	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/api/handlers/admin"
	fedhandler "hubplay/internal/api/handlers/federation"
	"hubplay/internal/api/handlers/me"
	"hubplay/internal/api/handlers/media"
	"hubplay/internal/auth"
	"hubplay/internal/federation"
)

// mountFederationPublic registra las dos clases de rutas federation
// fuera de la sesión del usuario:
//
//  1. Verdaderamente públicas — /federation/info, identity avatar, y
//     /peer/handshake. Los handshakes autentican por invite code en
//     el body; el info es público a propósito para que un peer pueda
//     descubrir nuestra identidad antes del pairing.
//
//  2. Peer-autenticadas — todo lo demás bajo /peer/* va detrás del
//     middleware RequirePeerJWT (Ed25519 firmado por el peer, audience
//     = nuestro server_uuid). El mismo middleware aplica el rate-limit
//     per-peer y registra todo en el audit log.
func mountFederationPublic(r chi.Router, deps Dependencies, fedImgSrv *media.ImageHandler) {
	if deps.Federation == nil {
		return
	}
	pubFed := fedhandler.NewFederationPublicHandler(deps.Federation, deps.Logger)
	r.Get("/federation/info", pubFed.ServerInfo)
	// Foto del servidor — público a propósito: los peers la consumen
	// sin firmar JWT desde el avatar_image_url que reciben en
	// /federation/info.
	r.Get("/federation/identity/avatar", pubFed.ServeIdentityAvatar)
	r.Post("/peer/handshake", pubFed.Handshake)

	// Pairing requests "Steam-style" (migration 048). Tres endpoints
	// públicos en par a /peer/handshake:
	//   POST /federation/pairing-requests          (A -> B inicial)
	//   POST /federation/pairing-requests/{id}/callback (B -> A)
	//   POST /federation/pairing-requests/{id}/cancel   (A -> B)
	// La autorización va por contenido (request_token + firma Ed25519
	// cuando aplica), no por JWT del peer — el JWT sólo existe DESPUÉS
	// del pairing.
	//
	// Rate-limit per-IP: 5 req/min/IP, burst 3. Defensa vs flood; el
	// admin toggle "accept_pairing_requests" + el cap de incoming
	// pending son las otras dos capas.
	pairingRL := handlers.NewPairingRequestRateLimiter()
	r.Group(func(r chi.Router) {
		r.Use(handlers.IPRateLimitMiddleware(pairingRL))
		r.Post("/federation/pairing-requests", pubFed.ReceivePairingRequest)
		r.Post("/federation/pairing-requests/{id}/callback", pubFed.ReceivePairingCallback)
		r.Post("/federation/pairing-requests/{id}/cancel", pubFed.ReceivePairingCancel)
	})

	r.Group(func(r chi.Router) {
		r.Use(federation.RequirePeerJWT(deps.Federation))
		r.Get("/peer/ping", pubFed.Ping)
		// Catalog browse (Phase 3) — JOIN-filtered contra
		// federation_library_shares server-side. Un peer nunca ve
		// libraries / items que no tenga compartidos.
		r.Get("/peer/libraries", pubFed.ListLibraries)
		r.Get("/peer/libraries/{libraryID}/items", pubFed.ListLibraryItems)
		r.Get("/peer/search", pubFed.SearchLibraries)
		r.Get("/peer/recent", pubFed.ListRecent)

		// Streaming (Phase 5). Peer A nos pide spawnar una sesión
		// de stream sobre uno de nuestros items; servimos los
		// manifests HLS + segments contra el UUID opaco de sesión
		// resultante. Ambas ACL gated por share.CanPlay -- el
		// session UUID solo no es suficiente.
		if deps.StreamManager != nil && deps.Items != nil {
			fedStream := fedhandler.NewFederationStreamHandler(deps.Federation, deps.StreamManager, deps.Items, deps.MediaStreams, deps.Logger)
			r.Post("/peer/stream/{itemId}/session", fedStream.StartSession)
			r.Get("/peer/stream/session/{sessionId}/master.m3u8", fedStream.MasterPlaylist)
			// Subtitles ANTES de las wildcard {quality}/* para que
			// el segmento literal `subtitles` gane el match (chi
			// prefiere literal sobre param a la misma profundidad,
			// pero mantener el orden de registro explícito evita
			// sorpresas si cambia la lógica de routing).
			r.Get("/peer/stream/session/{sessionId}/subtitles", fedStream.Subtitles)
			r.Get("/peer/stream/session/{sessionId}/subtitles/{trackIndex}", fedStream.SubtitleTrack)
			r.Get("/peer/stream/session/{sessionId}/{quality}/index.m3u8", fedStream.QualityPlaylist)
			r.Get("/peer/stream/session/{sessionId}/{quality}/{segment}", fedStream.Segment)
		}

		// Poster proxy (Phase 5 Slice 2). El catalog UI del peer
		// pide cada poster aquí para que los usuarios del peer
		// nunca contacten a este server directo (no IP / UA leak)
		// y para que podamos revalidar CanBrowse en cada fetch
		// (un peer que perdió un share desde el último cache local
		// no puede seguir pulling artwork).
		if deps.Items != nil && deps.Images != nil && fedImgSrv != nil {
			fedImg := fedhandler.NewFederationImageHandler(deps.Federation, deps.Items, deps.Images, fedImgSrv, deps.Logger)
			r.Get("/peer/items/{itemId}/poster", fedImg.ItemPoster)
		}
	})
}

// mountAdminAuthAndFederation registra los tres bloques que el código
// original engancha bajo el mismo guard `ks != nil`: signing keys
// (/admin/auth/keys), federation cliente (/me/peers/*), y federation
// admin (/admin/peers/*). Cuando el keystore no está disponible (tests
// minimalistas) los tres se omiten conjuntamente.
func mountAdminAuthAndFederation(r chi.Router, deps Dependencies) {
	ks := deps.Auth.KeyStoreOrNil()
	if ks == nil {
		return
	}

	// Signing key lifecycle (owner-only). Cada ruta aquí es
	// destructiva — gateadas a nivel de grupo para que un solo
	// cambio de middleware toggle el acceso para todas.
	var observe func(outcome string)
	if deps.Metrics != nil {
		observe = func(outcome string) {
			deps.Metrics.AuthKeyRotations.WithLabelValues(outcome).Inc()
		}
	}
	adminAuth := admin.NewAdminAuthHandler(ks, nil, observe, deps.Logger)

	r.Route("/admin/auth/keys", func(r chi.Router) {
		// Owner-only (migración 055): JWT signing keys protegen la
		// autenticación de todo el server. Rotar/podar es una
		// operación que sólo el dueño de la instalación debería tocar.
		if deps.Permissions != nil {
			r.Use(deps.Permissions.RequireOwner)
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Get("/", adminAuth.ListKeys)
		r.Post("/rotate", adminAuth.Rotate)
		r.Post("/prune", adminAuth.Prune)
	})

	if deps.Federation == nil {
		return
	}

	// User-facing federation surface — cualquier usuario auth'd
	// puede explorar lo que el admin compartió con peers (Phase 4).
	// El server usa peer JWTs internamente; el usuario sólo lleva su
	// session token normal.
	mePeers := me.NewMePeersHandler(deps.Federation, deps.Logger)
	r.Route("/me/peers", func(r chi.Router) {
		r.Get("/", mePeers.ListMyPeers)
		// Vista unificada: todas las libraries de todos los peers
		// paired en una respuesta, usada por la landing /peers
		// para que la UI no tenga que hacer fan-out N llamadas.
		r.Get("/libraries", mePeers.BrowseAllPeerLibraries)
		// Federated search: fan-out la query a todos los peers
		// paired en paralelo y agregamos los hits con metadatos
		// de origen. Per-peer timeouts dentro del manager evitan
		// que un peer lento bloquee la respuesta.
		r.Get("/search", mePeers.SearchPeers)
		// Cross-peer "what's new?" rail: fan-out a todos los peers
		// paired por sus items más frescos. Misma postura que
		// /search (per-peer timeout, errors-skip, fairness cap).
		r.Get("/recent", mePeers.RecentPeers)
		// Cross-peer Continue Watching: lee federation_progress
		// JOIN federation_item_cache local, sin fan-out (el state
		// es nuestro).
		r.Get("/continue-watching", mePeers.PeerContinueWatching)
		r.Get("/{peerID}/libraries", mePeers.BrowsePeerLibraries)
		r.Get("/{peerID}/libraries/{libraryID}/items", mePeers.BrowsePeerItems)
		r.Post("/{peerID}/libraries/{libraryID}/refresh", mePeers.RefreshPeerLibrary)
		// Poster proxy. El <img src> del PosterCard pega aquí y
		// hacemos relay de los bytes del peer con nuestro peer
		// JWT. Same-origin (no CORS), y el peer nunca ve la IP /
		// UA del usuario.
		r.Get("/{peerID}/items/{itemId}/poster", mePeers.ProxyPeerItemPoster)
		// Streaming proxy (Phase 5). El HLS player del usuario
		// sólo habla con nosotros; proxieamos los bytes del peer
		// con nuestro peer JWT.
		r.Post("/{peerID}/stream/{itemId}/session", mePeers.StartPeerStreamSession)
		r.Get("/{peerID}/stream/session/{sessionId}/master.m3u8", mePeers.ProxyPeerStreamMaster)
		r.Get("/{peerID}/stream/session/{sessionId}/subtitles", mePeers.ProxyPeerStreamSubtitles)
		r.Get("/{peerID}/stream/session/{sessionId}/subtitles/{trackIndex}", mePeers.ProxyPeerStreamSubtitleTrack)
		r.Get("/{peerID}/stream/session/{sessionId}/{quality}/index.m3u8", mePeers.ProxyPeerStreamQuality)
		r.Get("/{peerID}/stream/session/{sessionId}/{quality}/{segment}", mePeers.ProxyPeerStreamSegment)
		// Cross-peer playback state para un item. Misma forma que
		// /me/items/{id}/progress pero scoped a (peer,
		// remote_item_id) y backed por federation_progress
		// (migration 028).
		r.Get("/{peerID}/items/{itemId}/progress", mePeers.GetPeerItemProgress)
		r.Post("/{peerID}/items/{itemId}/progress", mePeers.UpdatePeerItemProgress)
	})

	// Federation admin surface — invite generation, peer pairing,
	// peer listing, peer revocation.
	adminFed := fedhandler.NewFederationAdminHandler(deps.Federation, deps.Logger)
	r.Route("/admin/peers", func(r chi.Router) {
		// Owner-only (migración 055): pairing con peers remotos
		// abre superficie de salida de datos (catálogo, posters
		// proxied). Operación de instalación, no de admin del día
		// a día.
		if deps.Permissions != nil {
			r.Use(deps.Permissions.RequireOwner)
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Get("/", adminFed.ListPeers)
		r.Get("/identity", adminFed.GetServerIdentity)
		r.Put("/identity", adminFed.UpdateServerIdentity)
		// Toggles admin de federation (anti-spam, etc.).
		r.Get("/settings", adminFed.GetFederationSettings)
		r.Put("/settings", adminFed.UpdateFederationSettings)
		// Foto del servidor: upload multipart + delete idempotente.
		// El serve público vive bajo /federation/identity/avatar
		// (sin auth).
		r.Post("/identity/avatar", adminFed.UploadServerAvatar)
		r.Delete("/identity/avatar", adminFed.DeleteServerAvatar)
		r.Post("/probe", adminFed.ProbePeer)
		r.Post("/accept", adminFed.AcceptInvite)
		// Pairing requests Steam-style: 5 admin endpoints
		// (migration 048). Reemplazan funcionalmente el flow
		// Invite + AcceptInvite + handshake para admins que
		// prefieran "0 copy-paste".
		r.Route("/pairing-requests", func(r chi.Router) {
			r.Get("/", adminFed.ListPairingRequests)
			r.Post("/send", adminFed.SendPairingRequest)
			r.Post("/{id}/accept", adminFed.AcceptPairingRequest)
			r.Post("/{id}/decline", adminFed.DeclinePairingRequest)
			r.Delete("/{id}", adminFed.CancelPairingRequest)
		})
		r.Get("/{id}", adminFed.GetPeer)
		r.Post("/{id}/refresh", adminFed.RefreshPeer)
		r.Delete("/{id}", adminFed.RevokePeer)
		r.Route("/invites", func(r chi.Router) {
			r.Get("/", adminFed.ListActiveInvites)
			r.Post("/", adminFed.GenerateInvite)
		})
		r.Route("/{id}/shares", func(r chi.Router) {
			r.Get("/", adminFed.ListShares)
			r.Post("/", adminFed.CreateShare)
			r.Delete("/{shareID}", adminFed.DeleteShare)
		})
	})
}
