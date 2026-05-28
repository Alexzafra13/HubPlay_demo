package model

import "time"

// ProviderConfig es la configuración persistida de un metadata/image/
// subtitle provider (TMDb, Fanart.tv, OpenSubtitles). Extraído de
// db/ para que handlers y provider manager no importen la capa de
// persistencia.
// ProviderConfig es la configuración persistida de un metadata/image/subtitle
// provider. Alias: era db.ProviderConfig; movido aquí para que el
// paquete provider sea dueño de su tipo de dominio (PP).
type ProviderConfig struct {
	Name       string
	Type       string // metadata, image, subtitle
	Version    string
	Status     string // active, disabled
	Priority   int
	ConfigJSON string
	APIKey     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
