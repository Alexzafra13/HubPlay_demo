package library

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler escanea periódicamente libraries según su scan_interval.
type Scheduler struct {
	service  *Service
	logger   *slog.Logger
	stopCh   chan struct{}
	interval time.Duration
}

func NewScheduler(service *Service, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		service:  service,
		logger:   logger.With("module", "scheduler"),
		stopCh:   make(chan struct{}),
		interval: 15 * time.Minute,
	}
}

// Start lanza el loop de scheduling. Escanea todas las libraries auto-mode
// al arrancar (como Jellyfin), luego comprueba periódicamente.
func (s *Scheduler) Start(ctx context.Context) {
	s.logger.Info("library scan scheduler started", "check_interval", s.interval)

	go func() {
		time.Sleep(5 * time.Second)
		s.logger.Info("running startup scan for all auto-mode libraries")
		s.service.ScanAll(ctx)
	}()

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.runDueScans(ctx)
			case <-s.stopCh:
				s.logger.Info("scheduler stopped")
				return
			case <-ctx.Done():
				s.logger.Info("scheduler context cancelled")
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) runDueScans(ctx context.Context) {
	libs, err := s.service.List(ctx)
	if err != nil {
		s.logger.Error("scheduler: failed to list libraries", "error", err)
		return
	}

	for _, lib := range libs {
		if lib.ScanMode == "manual" {
			continue
		}

		interval, err := time.ParseDuration(lib.ScanInterval)
		if err != nil {
			interval = 6 * time.Hour
		}

		if time.Since(lib.UpdatedAt) < interval {
			continue
		}

		s.logger.Info("scheduled scan due", "library", lib.Name, "interval", interval)
		if err := s.service.Scan(ctx, lib.ID); err != nil {
			s.logger.Warn("scheduled scan failed to start", "library", lib.Name, "error", err)
		}
	}
}
