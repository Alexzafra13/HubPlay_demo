package handlers

// Cache-Control headers usados across handlers. Centralizados aquí para
// que cambiar un TTL sea una sola edición en vez de grep-y-replace por
// 22 ficheros — y para que en review se lea el nombre semántico (`CacheControlImage`)
// en vez de un magic string (`"public, max-age=86400, stale-while-revalidate=604800"`).
// Cierra olor F14-9-a del audit 2026-05-14.
//
// Reglas de uso:
//
//   - `NoCache` para SSE y endpoints admin que NUNCA deben cachear
//     respuestas. Cambia con cada request.
//   - `NoStore` añade restricción contra persistencia en disco
//     (backup downloads, DB driver swap responses).
//   - `NoStoreFull` (HLS playlists efímeras) es el de IPTV stream
//     proxy: además de `no-cache,no-store` añade `must-revalidate`
//     para defender contra proxies que ignoran `no-cache`.
//   - Los TTLs públicos (`ShortLived`/`Hourly`/`Daily*`) requieren
//     que el contenido sea content-addressed o suficientemente
//     estable como para tolerar staleness — verificar antes de
//     aplicar a un endpoint nuevo.
//   - `Image` añade `stale-while-revalidate=604800` para servir el
//     blob viejo durante una semana mientras background refresh —
//     baja el latency P99 de servir thumbnails que están en re-fetch.
const (
	CacheControlNoCache       = "no-cache"
	CacheControlNoStore       = "no-store"
	CacheControlNoStoreFull   = "no-cache, no-store, must-revalidate"
	CacheControlShortLived    = "public, max-age=10"
	CacheControlListingShort  = "private, max-age=15"
	CacheControlListing       = "private, max-age=30"
	CacheControlMediumPublic  = "public, max-age=300"
	CacheControlHourly        = "max-age=3600"
	CacheControlHourlyPublic  = "public, max-age=3600"
	CacheControlDailyPublic   = "public, max-age=86400"
	CacheControlDailyOpaque   = "max-age=86400"
	CacheControlImage         = "public, max-age=86400, stale-while-revalidate=604800"
)
