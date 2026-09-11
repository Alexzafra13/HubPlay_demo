// Package discovery implementa el descubrimiento del servidor en la LAN
// por sondeo UDP (broadcast), complementario al anuncio mDNS de
// internal/mdns.
//
// Por qué un segundo mecanismo: mDNS es multicast y NO atraviesa el
// bridge de Docker — con `ports: "8097:8096"` el anuncio se queda dentro
// del contenedor y ningún cliente de la casa lo ve. Un broadcast UDP a un
// puerto publicado sí llega (docker-proxy escucha en 0.0.0.0 y reenvía),
// y la respuesta vuelve por el mismo camino con la IP del host como
// origen. Es el mismo enfoque que usa Plex (GDM) para que la app de TV
// encuentre el servidor sin teclear nada.
//
// Protocolo (v1), deliberadamente mínimo:
//
//	cliente → broadcast UDP/41860: "HUBPLAY-DISCOVER/1"
//	servidor → unicast al origen:  {"product":"hubplay","name":"HubPlay",
//	                                "version":"…","port":8097}
//
// El cliente construye la URL con la IP de origen del datagrama y `port`.
// `port` es el puerto ALCANZABLE desde la LAN (con Docker, el del host,
// que el contenedor no conoce: se le pasa por config/env
// HUBPLAY_DISCOVERY_ADVERTISE_PORT); si no se configura, el del propio
// servidor HTTP.
package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
)

// Magic es el prefijo que debe llevar el datagrama de sondeo. Cualquier
// otro tráfico que llegue al puerto se ignora en silencio.
const Magic = "HUBPLAY-DISCOVER/1"

// DefaultPort es el puerto UDP de escucha por defecto. Fuera del rango
// de puertos bien conocidos y sin uso registrado en IANA.
const DefaultPort = 41860

// maxProbe acota la lectura: un sondeo legítimo ocupa 18 bytes.
const maxProbe = 256

// Config del respondedor.
type Config struct {
	// Enabled controla si escuchamos. Default true (en config).
	Enabled bool
	// Port UDP de escucha. 0 → DefaultPort.
	Port int
	// HTTPPort es el puerto del servidor HTTP (cfg.Server.Port). Se
	// anuncia si AdvertisePort es 0.
	HTTPPort int
	// AdvertiseURL, si se configura, se envía tal cual y el cliente la usa
	// en vez de construir http://<ip>:<port>. Para instalaciones detrás de
	// un proxy TLS (deploy/docker-compose.prod.yml) donde el puerto HTTP
	// no está expuesto a la LAN: p.ej. "https://hubplay.duckdns.org".
	AdvertiseURL string
	// AdvertisePort es el puerto que los clientes de la LAN deben usar
	// para llegar al servidor HTTP. Con Docker es el puerto del host
	// (`ports: "8097:8096"` → 8097).
	AdvertisePort int
	// Name legible del servidor en la lista de la app. Default "HubPlay".
	Name string
	// Version del binario, informativa.
	Version string
}

// Reply es el cuerpo JSON que devolvemos a cada sondeo.
type Reply struct {
	Product string `json:"product"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Port    int    `json:"port"`
	URL     string `json:"url,omitempty"`
}

// Responder es el listener UDP en marcha.
type Responder struct {
	conn   *net.UDPConn
	reply  []byte
	logger *slog.Logger
}

// Start abre el socket UDP y atiende sondeos hasta que ctx se cancela.
// Devuelve nil, nil si cfg.Enabled es false. Un fallo al abrir el puerto
// no debe tumbar el servidor: el caller lo registra como warning.
func Start(ctx context.Context, cfg Config, logger *slog.Logger) (*Responder, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("discovery: invalid port %d", cfg.Port)
	}
	if cfg.Name == "" {
		cfg.Name = "HubPlay"
	}
	advertise := cfg.AdvertisePort
	if advertise == 0 {
		advertise = cfg.HTTPPort
	}
	if advertise <= 0 {
		return nil, fmt.Errorf("discovery: http port required")
	}

	reply, err := json.Marshal(Reply{
		Product: "hubplay",
		Name:    cfg.Name,
		Version: cfg.Version,
		Port:    advertise,
		URL:     cfg.AdvertiseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("discovery: marshal reply: %w", err)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: cfg.Port})
	if err != nil {
		return nil, fmt.Errorf("discovery: listen udp/%d: %w", cfg.Port, err)
	}

	r := &Responder{conn: conn, reply: reply, logger: logger}
	go r.serve()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	logger.Info("lan discovery responder started",
		"udp_port", r.Port(), "advertise_http_port", advertise, "advertise_url", cfg.AdvertiseURL)
	return r, nil
}

// Port devuelve el puerto UDP real (útil cuando se arranca con 0 en tests).
func (r *Responder) Port() int {
	if addr, ok := r.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

func (r *Responder) serve() {
	buf := make([]byte, maxProbe)
	magic := []byte(Magic)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			// Close() por cancelación del ctx: salida limpia.
			return
		}
		if !bytes.HasPrefix(buf[:n], magic) {
			continue
		}
		if _, err := r.conn.WriteToUDP(r.reply, from); err != nil {
			r.logger.Debug("discovery reply failed", "to", from.String(), "error", err)
		}
	}
}
