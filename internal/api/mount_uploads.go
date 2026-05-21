package api

import (
	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	authmodel "hubplay/internal/auth/model"
)

// mountUploads registra las tres superficies del feature upload (PR2):
//
//	POST/PATCH/HEAD/DELETE /uploads/         → protocolo tus
//	GET                    /uploads/mine     → audit del usuario
//	GET                    /uploads/events   → SSE filtrado
//
// El tus handler se monta con chi.Mount para que el path-routing
// (basePath + uploadID) lo lleve tusd internamente sin que chi le pise
// el id como param. Bajo el mismo bloque registra el upload-folder
// explorer (PR6) gateado por can_upload.
func mountUploads(r chi.Router, deps Dependencies) {
	if deps.Uploads == nil || deps.UploadsAudit == nil || deps.EventBus == nil {
		return
	}
	uploadsAPI := handlers.NewUploadsHandler(deps.UploadsAudit, deps.EventBus, deps.SSELimiter, deps.Logger)
	r.Get("/uploads/mine", uploadsAPI.ListMine)
	r.Get("/uploads/events", uploadsAPI.Stream)
	// tus handler. Importante: bajo /api/v1/uploads/ con el slash
	// final — tusd compone Location: /api/v1/uploads/<id> tras el
	// POST de creación, y el cliente PATCH-ea ahí mismo.
	r.Mount("/uploads/", deps.Uploads)

	// Upload folder explorer (PR6 file explorer). Gateado por
	// can_upload — un user sin permiso de subir no necesita el
	// browser. Owner pasa automático via User.Can().
	if deps.Libraries == nil {
		return
	}
	browseHandler := handlers.NewUploadBrowseHandler(deps.Libraries, deps.Logger)
	r.Group(func(r chi.Router) {
		if deps.Permissions != nil {
			r.Use(deps.Permissions.Require(authmodel.PermUpload))
		}
		r.Get("/libraries/{id}/upload-browse", browseHandler.Browse)
		r.Post("/libraries/{id}/folders", browseHandler.CreateFolder)
		r.Delete("/libraries/{id}/files", browseHandler.DeleteEntry)
		r.Post("/libraries/{id}/files/rename", browseHandler.RenameEntry)
	})
}
