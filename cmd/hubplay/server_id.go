package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

// settingsStore es lo que loadOrCreateServerID necesita de repos.Settings.
type settingsStore interface {
	GetOr(ctx context.Context, key, def string) (string, error)
	Set(ctx context.Context, key, value string) error
}

const serverIDKey = "server.instance_id"

// loadOrCreateServerID devuelve un identificador estable y público de
// esta instalación (16 hex), generado una vez y guardado en app_settings.
// Lo anuncian mDNS, el respondedor UDP y /health para que la app de TV
// reconozca el mismo servidor aunque conteste por dos IPs (Wi-Fi + cable,
// Docker + host…). Si algo falla devuelve "" y la app cae al comportamiento
// anterior (una entrada por IP).
func loadOrCreateServerID(ctx context.Context, settings settingsStore, logger *slog.Logger) string {
	id, err := settings.GetOr(ctx, serverIDKey, "")
	if err != nil {
		logger.Warn("server id: read failed", "error", err)
		return ""
	}
	if id != "" {
		return id
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		logger.Warn("server id: random failed", "error", err)
		return ""
	}
	id = hex.EncodeToString(b[:])
	if err := settings.Set(ctx, serverIDKey, id); err != nil {
		logger.Warn("server id: persist failed", "error", err)
		return ""
	}
	return id
}
