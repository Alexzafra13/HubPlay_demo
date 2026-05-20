// Package mdns anuncia HubPlay en la LAN vía multicast DNS.
//
// Dispositivos en la misma red (Windows 10+, macOS, móviles modernos)
// resuelven el hostname configurado (`hubplay.local` por defecto) sin
// tocar el router ni DNS. Es lo que hace Plex con `*.plex.direct` —
// menos lo de los certs TLS, que queda fuera de scope.
//
// El announcer es opt-out: si Config.Enabled=false no arranca. No
// hacemos failover loco si el puerto multicast UDP/5353 está bloqueado
// por firewall — devuelve error al construir, el caller decide si
// loguea warn y sigue (lo hacemos así desde main.go).
package mdns

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/grandcat/zeroconf"
)

type Config struct {
	// Enabled controla si arrancamos el announcer.
	Enabled bool
	// Hostname queda como "<hostname>.local" en la red. Default "hubplay".
	Hostname string
	// InstanceName es el nombre legible que ve un cliente Bonjour-aware
	// (e.g. una app de descubrimiento). Default "HubPlay".
	InstanceName string
	// Port HTTP del server (igual que cfg.Server.Port).
	Port int
	// Version inyectada en el TXT record para que clientes puedan
	// filtrar por versión sin tener que llamar al servidor.
	Version string
}

type Announcer struct {
	server *zeroconf.Server
	logger *slog.Logger
}

// Start registra el servicio _http._tcp con el hostname forzado a
// "<Config.Hostname>.local". Devuelve nil, nil si Enabled=false.
func Start(ctx context.Context, cfg Config, logger *slog.Logger) (*Announcer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "hubplay"
	}
	if cfg.InstanceName == "" {
		cfg.InstanceName = "HubPlay"
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("mdns: port required")
	}

	txt := []string{
		"path=/",
		"version=" + cfg.Version,
	}
	// RegisterProxy fuerza el hostname (en vez de usar el del SO).
	srv, err := zeroconf.RegisterProxy(
		cfg.InstanceName,
		"_http._tcp",
		"local.",
		cfg.Port,
		cfg.Hostname,
		nil,
		txt,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}

	logger.Info("mdns announcer started",
		"host", cfg.Hostname+".local",
		"instance", cfg.InstanceName,
		"port", cfg.Port)

	a := &Announcer{server: srv, logger: logger.With("module", "mdns")}
	go func() {
		<-ctx.Done()
		srv.Shutdown()
		a.logger.Info("mdns announcer stopped")
	}()
	return a, nil
}
