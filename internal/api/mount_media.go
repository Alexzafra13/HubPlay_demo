package api

import (
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

// mountStreaming registra el surface de player: master playlist HLS,
// per-quality playlist + segmento, direct play, stop session,
// subtitles (internos + external providers tipo OpenSubtitles).
func mountStreaming(r chi.Router, deps Dependencies) {
	if deps.StreamManager == nil {
		return
	}
	streamHandler := handlers.NewStreamHandler(
		deps.StreamManager, deps.Items, deps.MediaStreams,
		deps.ExternalIDs, deps.Providers,
		deps.Settings, deps.ServerBaseURL, deps.Logger,
	)

	r.Route("/stream/{itemId}", func(r chi.Router) {
		r.Get("/info", streamHandler.Info)
		r.Get("/master.m3u8", streamHandler.MasterPlaylist)
		r.Get("/{quality}/index.m3u8", streamHandler.QualityPlaylist)
		r.Get("/{quality}/{segment}", streamHandler.Segment)
		r.Get("/direct", streamHandler.DirectPlay)
		r.Delete("/session", streamHandler.StopSession)
		r.Get("/subtitles", streamHandler.Subtitles)
		r.Get("/subtitles/{trackIndex}", streamHandler.SubtitleTrack)
		// External subtitle providers (OpenSubtitles, ...).
		// Search devuelve candidatos; el download endpoint pipea
		// el SRT/ASS por ffmpeg → WebVTT y lo sirve para el
		// <track> element del player.
		r.Get("/subtitles/external", streamHandler.SearchExternalSubtitles)
		r.Get("/subtitles/external/{fileId}", streamHandler.DownloadExternalSubtitle)
	})
}

// mountLibrariesItemsAndIPTV registra los tres ejes media-centric del
// router: bibliotecas (CRUD + browse), items (detalle, search, genres,
// recommendations, trickplay, identify, metadata edit), e IPTV
// (channels, EPG, favorites, admin curation). Los tres comparten el
// libHandler para que pre-construir el handler una vez y reusarlo
// downstream sea posible.
func mountLibrariesItemsAndIPTV(r chi.Router, deps Dependencies, fedImageDir string) {
	if deps.Libraries == nil {
		return
	}
	libHandler := handlers.NewLibraryHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Users, deps.Audit, deps.Logger)
	// Trickplay sprites aterrizan bajo <imageDir>/trickplay/ —
	// reusar el image-storage root mantiene el on-disk layout
	// clustered (un solo tree que el operador puede backup,
	// rsync, o `du` para sizearle el cache).
	trickplayDir := filepath.Join(deps.DataDir, "images", "trickplay")
	// scanner ↔ MetadataIdentifier: deps.Scanner es *scanner.Scanner;
	// el handler sólo necesita la pequeña interfaz MetadataIdentifier.
	// Pasarlo como nil cuando no esté wired hace que los endpoints
	// /identify devuelvan 503 sin tumbar el resto del handler.
	var identifier handlers.MetadataIdentifier
	if deps.Scanner != nil {
		identifier = deps.Scanner
	}
	itemHandler := handlers.NewItemHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Users, deps.Chapters, deps.EpisodeSegments, deps.ExternalIDs, deps.People, deps.Collections, deps.Providers, identifier, trickplayDir, deps.Audit, deps.Logger)

	// Libraries
	r.Get("/libraries", libHandler.List)
	r.Route("/libraries/{id}", func(r chi.Router) {
		r.Get("/", libHandler.Get)
		r.Get("/items", libHandler.Items)

		// Library mutations (migración 055): can_manage_libraries.
		r.Group(func(r chi.Router) {
			if deps.Permissions != nil {
				r.Use(deps.Permissions.Require(authmodel.PermManageLibraries))
			} else {
				r.Use(auth.RequireAdmin)
			}
			r.Put("/", libHandler.Update)
			r.Delete("/", libHandler.Delete)
			r.Post("/scan", libHandler.Scan)
		})
	})
	r.Group(func(r chi.Router) {
		// Library create / browse (migración 055): can_manage_libraries.
		if deps.Permissions != nil {
			r.Use(deps.Permissions.Require(authmodel.PermManageLibraries))
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Post("/libraries", libHandler.Create)
		r.Get("/libraries/browse", libHandler.Browse)
	})

	// IPTV channels (within library routes)
	if deps.IPTV != nil {
		mountIPTVChannels(r, deps, fedImageDir)
	}

	// Items
	r.Get("/items/latest", libHandler.LatestItems)
	// Global paginated items list. Mismo payload shape que
	// /libraries/{id}/items pero spanning toda library — las
	// Movies / Series browse pages no pre-pickean library así
	// que no pueden ir por el scoped route. Sin esto las pages
	// caían a /items/latest que está capeado a 50 y no
	// paginates, lo que se veía como "sólo unas pocas movies"
	// en el browse grid.
	r.Get("/items", libHandler.AllItems)
	r.Get("/items/search", itemHandler.Search)
	// Catalogue-wide genre vocabulary para el filter panel.
	// Devuelve name + count, sorted by frequency desc, scoped
	// por ?type=movie|series así que una TV-only library no
	// surfacea "Action & Adventure" a /movies y vice versa.
	r.Get("/items/genres", libHandler.Genres)
	r.Route("/items/{id}", func(r chi.Router) {
		r.Get("/", itemHandler.Get)
		r.Get("/children", itemHandler.Children)
		// "More like this" rail. Pulls from TMDb recommendations
		// + cross-references cada candidato contra la library
		// local así que la UI puede deep-linkear a matches
		// in-library.
		r.Get("/recommendations", itemHandler.Recommendations)
		// Trickplay (seek-bar thumbnail previews). El primer
		// hit triggers ffmpeg generation; ambos endpoints sirven
		// from disk en hits subsequentes.
		r.Get("/trickplay.json", itemHandler.TrickplayManifest)
		r.Get("/trickplay.png", itemHandler.TrickplaySprite)

		// Identify / rematch contra TMDb (admin-only). Mismo
		// patrón Plex/Jellyfin: el operador busca, elige el
		// match correcto y se aplica sobrescribiendo metadatos
		// + imágenes del item.
		// Item identify + metadata edits (migración 055):
		// can_edit_metadata. Cubre el flujo Plex-style de
		// rematch contra TMDb + el editor manual.
		r.Group(func(r chi.Router) {
			if deps.Permissions != nil {
				r.Use(deps.Permissions.Require(authmodel.PermEditMetadata))
			} else {
				r.Use(auth.RequireAdmin)
			}
			r.Get("/identify/candidates", itemHandler.IdentifyCandidates)
			r.Post("/identify", itemHandler.Identify)
			// Editor manual de metadatos. Distinto de identify:
			// no consulta TMDb, sólo escribe los campos que el
			// operador suministra. Bloquea el item al guardar
			// para que el siguiente "Refresh metadata" no pise
			// la edición.
			r.Patch("/metadata", itemHandler.UpdateItemMetadata)
			r.Put("/metadata/lock", itemHandler.SetMetadataLock)
			// Re-corre el enrich del scanner sobre este item
			// (mismo flujo que el library refresh, pero para un
			// solo item). Lo dispara el kebab "Actualizar
			// metadatos" del poster / del detalle. Respeta el
			// lock.
			r.Post("/refresh-metadata", itemHandler.RefreshItemMetadata)
		})
	})
}

// mountIPTVChannels es la parte "Live TV" de mountLibrariesItemsAndIPTV.
// Se extrajo en función propia para que el bloque IPTV (~120 LoC) no
// ahogue la lectura del flow principal libraries/items. Se llama desde
// dentro del scope de libraries y comparte deps.IPTV* + fedImageDir
// con el itemHandler/libHandler hermanos.
func mountIPTVChannels(r chi.Router, deps Dependencies, fedImageDir string) {
	// Pass deps.IPTVTransmux as-is — cuando es nil el handler cae
	// al raw passthrough proxy, que es el comportamiento correcto
	// degraded-pero-funcional para deployments HLS-only sin ffmpeg.
	iptvHandler := handlers.NewIPTVHandler(deps.IPTV, deps.IPTVProxy, deps.IPTVTransmux, deps.IPTVLogoCache, fedImageDir, deps.LibraryRepo, deps.Libraries, deps.Audit, deps.EventBus, deps.Logger)

	r.Route("/libraries/{id}/channels", func(r chi.Router) {
		r.Get("/", iptvHandler.ListChannels)
		r.Get("/groups", iptvHandler.Groups)
	})

	r.Route("/channels/{channelId}", func(r chi.Router) {
		r.Get("/", iptvHandler.GetChannel)
		r.Get("/stream", iptvHandler.Stream)
		r.Get("/proxy", iptvHandler.ProxyURL)
		r.Get("/schedule", iptvHandler.Schedule)
		r.Post("/watch", iptvHandler.RecordChannelWatch)
		r.Post("/playback-failure", iptvHandler.RecordPlaybackFailure)
		// HLS transmux endpoints. El Stream handler 302s aquí
		// cuando el upstream es MPEG-TS (Xtream Codes, raw
		// TS-over-HTTP). El manifest spawnea / re-usa la
		// sesión ffmpeg per-channel; segments se sirven del
		// work dir de la sesión. Ambos 404 gracefully cuando
		// no existe sesión así que hls.js recupera vía
		// reload del manifest.
		r.Get("/hls/index.m3u8", iptvHandler.HLSManifest)
		r.Get("/hls/{segment}", iptvHandler.HLSSegment)
		// Same-origin proxy para el tvg-logo del canal.
		// Mirrors la upstream image a disco + sirve desde el
		// local cache, así CSP puede quedarse locked a
		// `self` y los external image hosts no pueden
		// trackear al user.
		r.Get("/logo", iptvHandler.ChannelLogo)
	})

	r.Get("/channels/schedule", iptvHandler.BulkSchedule)
	r.Post("/channels/schedule", iptvHandler.BulkSchedule)

	// Continue watching rail (per-user). GET only — el beacon
	// es POST /channels/{id}/watch arriba.
	r.Get("/me/channels/continue-watching", iptvHandler.ListContinueWatching)

	// Per-user channel personalisation: reorder + hide channels
	// para la vista del caller sin afectar otros users o los
	// admin defaults.
	r.Put("/me/iptv/channels/order", iptvHandler.ReplaceChannelOrder)
	r.Delete("/me/iptv/channels/order", iptvHandler.ResetChannelOrder)
	r.Put("/me/iptv/channels/{channelId}/visibility", iptvHandler.SetChannelVisibility)

	// Channel favorites (per-user, requires auth; no admin role).
	r.Route("/favorites/channels", func(r chi.Router) {
		r.Get("/", iptvHandler.ListFavorites)
		r.Get("/ids", iptvHandler.ListFavoriteIDs)
		r.Put("/{channelId}", iptvHandler.AddFavorite)
		r.Delete("/{channelId}", iptvHandler.RemoveFavorite)
	})

	// Public IPTV
	r.Get("/iptv/public/countries", iptvHandler.PublicCountries)
	r.Get("/iptv/epg-catalog", iptvHandler.EPGCatalog)

	// Per-library EPG source list (read: user con library ACL;
	// mutations: admin-only, abajo).
	r.Get("/libraries/{id}/epg-sources", iptvHandler.ListEPGSources)

	// Unhealthy-channels admin surface: read gated por la
	// misma library ACL que el channel list; los endpoints de
	// mutación viven bajo el admin group abajo.
	r.Get("/libraries/{id}/channels/unhealthy", iptvHandler.ListUnhealthyChannels)
	r.Get("/libraries/{id}/channels/without-epg", iptvHandler.ListChannelsWithoutEPG)
	// Lightweight summary: sólo los tres counts que el admin
	// panel necesita en el first paint. Las heavy listas
	// unhealthy / without-epg cargan lazily, sólo cuando el
	// operador abre su tab.
	r.Get("/libraries/{id}/channels/health-summary", iptvHandler.ChannelHealthSummary)

	// IPTV scheduled jobs (M3U + EPG refresh automatizado).
	// Read: cualquier user con library ACL (para que el livetv
	// panel muestre el status del schedule). Mutations:
	// admin-only, en el group abajo.
	var iptvScheduleHandler *handlers.IPTVScheduleHandler
	if deps.IPTVSchedules != nil && deps.IPTVScheduler != nil {
		iptvScheduleHandler = handlers.NewIPTVScheduleHandler(
			deps.IPTVSchedules, deps.IPTVScheduler, deps.Libraries, deps.Logger)
		r.Get("/libraries/{id}/schedule", iptvScheduleHandler.List)
	}

	// Admin IPTV operations (migración 055): can_manage_iptv.
	r.Group(func(r chi.Router) {
		if deps.Permissions != nil {
			r.Use(deps.Permissions.Require(authmodel.PermManageIPTV))
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Post("/iptv/preflight", iptvHandler.PreflightM3U)
		r.Post("/iptv/public/import", iptvHandler.ImportPublicIPTV)
		r.Post("/libraries/{id}/epg-sources", iptvHandler.AddEPGSource)
		r.Delete("/libraries/{id}/epg-sources/{sourceId}", iptvHandler.RemoveEPGSource)
		r.Patch("/libraries/{id}/epg-sources/reorder", iptvHandler.ReorderEPGSources)
		r.Post("/channels/{channelId}/reset-health", iptvHandler.ResetChannelHealth)
		r.Post("/channels/{channelId}/disable", iptvHandler.DisableChannel)
		r.Post("/channels/{channelId}/enable", iptvHandler.EnableChannel)
		r.Patch("/channels/{channelId}", iptvHandler.PatchChannel)
		// Override del logo del canal (URL externa o archivo
		// subido). El GET del logo (proxy) NO está aquí — vive
		// arriba con los demás endpoints de canal porque
		// cualquier usuario autenticado lo pide; sólo escritura
		// es admin-only.
		r.Put("/channels/{channelId}/logo", iptvHandler.SetChannelLogo)
		r.Post("/channels/{channelId}/logo/upload", iptvHandler.UploadChannelLogo)
		r.Delete("/channels/{channelId}/logo", iptvHandler.ClearChannelLogo)
		// Admin channel curation. Reorder, hide, restore M3U
		// order. Hidden HERE is a hard constraint: downstream
		// el per-user overlay sólo puede esconder más, no
		// surfacear lo que el admin quitó.
		r.Get("/libraries/{id}/channels/admin-view", iptvHandler.ListLibraryChannelsAdmin)
		r.Put("/libraries/{id}/channels/order", iptvHandler.ReplaceLibraryChannelOrder)
		r.Delete("/libraries/{id}/channels/order", iptvHandler.ResetLibraryChannelOrder)
		r.Put("/libraries/{id}/channels/{channelId}/admin-visibility", iptvHandler.SetLibraryChannelVisibility)
		r.Route("/libraries/{id}/iptv", func(r chi.Router) {
			r.Post("/refresh-m3u", iptvHandler.RefreshM3U)
			r.Post("/refresh-epg", iptvHandler.RefreshEPG)
			// Auto-discovery de logos contra iptv-org (database
			// pública con miles de canales mapeados por tvg-id
			// → logo URL).
			r.Post("/refresh-logos-from-iptv-org", iptvHandler.RefreshLogosFromIPTVOrg)
		})
		if iptvScheduleHandler != nil {
			r.Put("/libraries/{id}/schedule/{kind}", iptvScheduleHandler.Upsert)
			r.Delete("/libraries/{id}/schedule/{kind}", iptvScheduleHandler.Delete)
			r.Post("/libraries/{id}/schedule/{kind}/run", iptvScheduleHandler.RunNow)
		}
	})
}

// mountImagesPeopleStudiosCollections registra el surface relacionado
// con imagen y metadatos colaterales: thumbnails y selects de carátula
// per item, serve de fichero local, perfiles de cast/crew, studios
// browse, y colecciones (sagas Jellyfin-style). Reusa el fedImgSrv
// compartido con el peer poster proxy para que ambos vean la misma
// path-mapping store + thumbnail cache.
func mountImagesPeopleStudiosCollections(r chi.Router, deps Dependencies, fedImgSrv *handlers.ImageHandler, fedImageDir string) {
	if deps.Images == nil || deps.Providers == nil || deps.ExternalIDs == nil || fedImgSrv == nil {
		return
	}
	imageDir := fedImageDir
	imgHandler := fedImgSrv

	// Image management (nested under items)
	r.Route("/items/{id}/images", func(r chi.Router) {
		r.Get("/", imgHandler.List)
		r.Get("/available", imgHandler.Available)
		r.Put("/{type}/select", imgHandler.Select)
		r.Post("/{type}/upload", imgHandler.Upload)
		r.Put("/{imageId}/primary", imgHandler.SetPrimary)
		r.Put("/{imageId}/lock", imgHandler.SetLocked)
		r.Delete("/{imageId}", imgHandler.Delete)
	})

	// Serve local image files
	r.Get("/images/file/{id}", imgHandler.ServeFile)

	// Serve cast/crew profile photos. Vive junto al image
	// endpoint normal así que el cache + auth context matchean
	// exacto. Los People IDs son uuids; el handler valida que
	// el resolved on-disk path se queda dentro de imageDir
	// antes de servir.
	if deps.People != nil {
		peopleHandler := handlers.NewPeopleHandler(deps.People, imageDir, deps.Logger)
		r.Get("/people/{id}", peopleHandler.Get)
		r.Get("/people/{id}/thumb", peopleHandler.Thumb)
	}

	// Studios browse + detail. Powers el click-on-the-studio-
	// mark flow en las páginas de detalle de movie/series —
	// /studios/{slug} devuelve el studio header (logo, name)
	// plus cada item de este catálogo linked a él, sorted
	// year-desc.
	if deps.Studios != nil {
		studioHandler := handlers.NewStudioHandler(deps.Studios, deps.Logger)
		r.Get("/studios", studioHandler.List)
		r.Get("/studios/{slug}", studioHandler.Get)
	}

	// Movie collections (sagas Jellyfin-style). Backed por el
	// record TMDb belongs_to_collection en cada movie;
	// /collections/{id} rendea los miembros de la saga en
	// release order bajo un hero pulled del propio poster +
	// backdrop de la collection.
	if deps.Collections != nil {
		var collectionOverrides handlers.CollectionImageOverrideRepo
		if deps.CollectionImageOverrides != nil {
			collectionOverrides = deps.CollectionImageOverrides
		}
		var collectionImages handlers.CollectionImageProvider
		if deps.Providers != nil {
			collectionImages = deps.Providers
		}
		collectionHandler := handlers.NewCollectionHandler(deps.Collections, collectionOverrides, collectionImages, fedImageDir, deps.Audit, deps.Logger)
		r.Get("/collections", collectionHandler.List)
		r.Get("/collections/{id}", collectionHandler.Get)
		// Cualquier usuario autenticado puede GET el archivo
		// (img-src 'self' del CSP del proyecto lo requiere).
		r.Get("/collections/{id}/images/{type}/file", collectionHandler.ServeCollectionImage)
		// Override de carátula/fondo (migración 055): can_change_artwork.
		r.Group(func(r chi.Router) {
			if deps.Permissions != nil {
				r.Use(deps.Permissions.Require(authmodel.PermChangeArtwork))
			} else {
				r.Use(auth.RequireAdmin)
			}
			r.Get("/collections/{id}/images/{type}/available", collectionHandler.AvailableCollectionImages)
			r.Put("/collections/{id}/images/{type}", collectionHandler.SetCollectionImage)
			r.Post("/collections/{id}/images/{type}/upload", collectionHandler.UploadCollectionImage)
			r.Delete("/collections/{id}/images/{type}", collectionHandler.ClearCollectionImage)
		})
	}

	// Admin: batch refresh imágenes por library
	// Batch image refresh por librería (migración 055):
	// can_change_artwork. Reescritura masiva de carátulas vía
	// TMDb/Fanart — cae en "cambiar artwork" pese a tocar
	// varios items a la vez.
	r.Group(func(r chi.Router) {
		if deps.Permissions != nil {
			r.Use(deps.Permissions.Require(authmodel.PermChangeArtwork))
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Post("/libraries/{id}/images/refresh", imgHandler.RefreshLibraryImages)
	})
}

// mountProviders registra el surface de providers (metadata, images,
// subtitles). La parte read-only la usa el flujo identify del item
// detail; la parte de management (List + Update) está gated por
// can_manage_libraries — el sourcing de metadatos es config de
// instalación de librerías.
func mountProviders(r chi.Router, deps Dependencies) {
	if deps.Providers == nil {
		return
	}
	providerHandler := handlers.NewProviderHandler(deps.Providers, deps.ProviderRepo, deps.Logger)

	r.Get("/providers/search/metadata", providerHandler.SearchMetadata)
	r.Get("/providers/metadata/{externalId}", providerHandler.GetMetadata)
	r.Get("/providers/images", providerHandler.GetImages)
	r.Get("/providers/search/subtitles", providerHandler.SearchSubtitles)

	// Provider management (migración 055): can_manage_libraries.
	// El sourcing de metadatos es config de instalación de
	// librerías (qué TMDb key usar, qué providers activar), así
	// que cae bajo la misma capability que crear y editar
	// librerías.
	r.Group(func(r chi.Router) {
		if deps.Permissions != nil {
			r.Use(deps.Permissions.Require(authmodel.PermManageLibraries))
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Get("/providers", providerHandler.List)
		r.Put("/providers/{name}", providerHandler.Update)
	})
}
