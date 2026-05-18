package notification

import (
	"context"
	"log/slog"
	"time"
)

// DefaultReadRetention es la ventana por defecto que mantenemos las
// notificaciones marcadas como leidas antes de borrarlas. 30 dias
// equilibra "el usuario las pierde si las marca leidas accidental"
// con "la tabla no crece para siempre".
const DefaultReadRetention = 30 * 24 * time.Hour

// StartReadCleanupSweeper lanza una goroutine que cada `interval`
// borra notificaciones leidas con read_at < ahora - retention. Las
// no-leidas se conservan siempre - el user todavia no las ha visto.
//
// Mismo patron que internal/db.StartPeriodicOptimize: ticker post-
// boot + cancel via ctx + log de cuantas borra.
func StartReadCleanupSweeper(ctx context.Context, repo *Repository, logger *slog.Logger, interval, retention time.Duration) func() {
	if repo == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if retention <= 0 {
		retention = DefaultReadRetention
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				before := time.Now().UTC().Add(-retention)
				n, err := repo.DeleteOldRead(ctx, before)
				if err != nil {
					logger.Warn("notifications: read-cleanup sweeper", "err", err)
					continue
				}
				if n > 0 {
					logger.Info("notifications: pruned old read notifications", "count", n, "retention", retention)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
