package main

import (
	"context"

	"hubplay/internal/audit"
	"hubplay/internal/db"
	"hubplay/internal/upload"
)

// Adapters de la frontera de persistencia para los stores de auditoría
// (cierre del olor SS-2). Los paquetes `audit` y `upload` definen sus
// propios structs espejo (audit.LogRow, upload.AuditRow) y ya no
// importan internal/db. La conversión a los tipos db.* vive aquí, en el
// composition root, que es el único sitio que conoce ambas capas.

// auditLogStore adapta *db.AuditLogRepository a audit.Store.
type auditLogStore struct{ repo *db.AuditLogRepository }

func (a auditLogStore) Insert(ctx context.Context, row audit.LogRow) error {
	return a.repo.Insert(ctx, db.AuditLogRow{
		ID:          row.ID,
		ActorUserID: row.ActorUserID,
		EventType:   row.EventType,
		TargetType:  row.TargetType,
		TargetID:    row.TargetID,
		Payload:     row.Payload,
		IPAddress:   row.IPAddress,
		UserAgent:   row.UserAgent,
		CreatedAt:   row.CreatedAt,
	})
}

// uploadAuditStore adapta *db.UploadAuditRepository a upload.AuditStore.
type uploadAuditStore struct{ repo *db.UploadAuditRepository }

func (a uploadAuditStore) Insert(ctx context.Context, row upload.AuditRow) error {
	return a.repo.Insert(ctx, db.UploadAuditRow{
		ID:           row.ID,
		UserID:       row.UserID,
		LibraryID:    row.LibraryID,
		OriginalName: row.OriginalName,
		FinalPath:    row.FinalPath,
		Bytes:        row.Bytes,
		SHA256:       row.SHA256,
		MimeDetected: row.MimeDetected,
		Outcome:      row.Outcome,
		ErrorMessage: row.ErrorMessage,
		StartedAt:    row.StartedAt,
		FinishedAt:   row.FinishedAt,
		DurationMs:   row.DurationMs,
	})
}
