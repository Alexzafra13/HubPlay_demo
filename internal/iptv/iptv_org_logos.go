package iptv

// IPTVOrgLogoLookup — descubrimiento automático de logos contra la
// base pública iptv-org (https://github.com/iptv-org/iptv). El JSON
// está en https://iptv-org.github.io/api/channels.json, ~5MB, miles de
// canales mapeados a logos por su id (que coincide con el tvg-id que
// los M3U usan).
//
// Flujo:
//   1. El admin pulsa "Buscar logos en iptv-org" en el panel de
//      curación de canales.
//   2. Si el JSON aún no está en disco, se descarga una vez. Refreshes
//      posteriores son explícitos — la base cambia despacio y forzar
//      el botón cada vez es más respetuoso con su CDN que un cron
//      silencioso por cada deploy.
//   3. Para cada canal sin tvg-logo y sin override admin, buscamos su
//      tvg_id en la base; si lo encontramos creamos una row en
//      channel_logo_overrides con la URL pública.
//
// El override se trata como cualquier otro — el admin puede "Restaurar
// logo del M3U" para borrarlo. iptv-org está documentado como fuente
// en la UI para que el operador sepa de dónde sale el logo.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const iptvOrgChannelsURL = "https://iptv-org.github.io/api/channels.json"

// iptvOrgChannel es el subset que necesitamos del JSON oficial. Hay
// muchos más campos (country, languages, broadcast_area...) que
// ignoramos — sólo id+logo son relevantes para este lookup.
type iptvOrgChannel struct {
	ID   string `json:"id"`
	Logo string `json:"logo"`
}

// IPTVOrgLogoLookup encapsula la descarga + cacheo en disco + parseo
// de la base de canales de iptv-org. Thread-safe (sólo lectura sobre
// el fichero cacheado).
type IPTVOrgLogoLookup struct {
	cachePath string
	client    *http.Client
}

// NewIPTVOrgLogoLookup construye el lookup. cachePath debe ser un path
// donde el proceso pueda escribir; típicamente <imageDir>/iptv-org-channels.json
// para que comparta tree con el resto del state persistente.
func NewIPTVOrgLogoLookup(cachePath string) *IPTVOrgLogoLookup {
	return &IPTVOrgLogoLookup{
		cachePath: cachePath,
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Refresh descarga el JSON actual desde iptv-org y reemplaza el cache
// local. No bloqueante en errores de I/O temporales (el caller puede
// reintentar); errores 4xx/5xx del servidor se propagan tal cual al
// admin.
func (l *IPTVOrgLogoLookup) Refresh(ctx context.Context) error {
	if l.cachePath == "" {
		return fmt.Errorf("iptv-org: cache path not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iptvOrgChannelsURL, nil)
	if err != nil {
		return err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch iptv-org channels: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iptv-org channels: HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(l.cachePath), 0o755); err != nil {
		return err
	}
	// Escritura atómica via tmp + rename — un crash a mitad de
	// download no deja un JSON corrupto que rompa parses futuros.
	tmp := l.cachePath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, l.cachePath)
}

// Load lee el cache en memoria y devuelve un map[tvg_id]→logo URL
// (claves en minúscula para tolerancia a variantes "BBC.uk" vs "bbc.uk"
// que aparecen entre M3Us). Si el cache no existe, lo descarga primero.
func (l *IPTVOrgLogoLookup) Load(ctx context.Context) (map[string]string, error) {
	if _, err := os.Stat(l.cachePath); errors.Is(err, os.ErrNotExist) {
		if err := l.Refresh(ctx); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(l.cachePath)
	if err != nil {
		return nil, err
	}
	var channels []iptvOrgChannel
	if err := json.Unmarshal(data, &channels); err != nil {
		return nil, fmt.Errorf("parse iptv-org channels: %w", err)
	}
	out := make(map[string]string, len(channels))
	for _, c := range channels {
		if c.ID == "" || c.Logo == "" {
			continue
		}
		out[strings.ToLower(c.ID)] = c.Logo
	}
	return out, nil
}
