-- +goose Up
-- Per-collection image overrides — el admin reemplaza el póster o el
-- fondo de una saga TMDb (Star Wars, MCU, Indiana Jones…) con su
-- propia URL o un archivo subido.
--
-- Modelo: una row por (collection_id, image_type) — image_type acepta
-- "poster" o "backdrop". Cada row tiene URL externa O archivo local
-- (uno de los dos, nunca ambos — CHECK lo enforca).
--
-- Por qué tabla nueva en vez de extender `collections`:
--   - La fila `collections` se regenera/upsert en cada scan que toca
--     una peli de la saga. Los campos `poster_url`/`backdrop_url` se
--     actualizan automáticamente desde TMDb. Si guardáramos el
--     override ahí, el siguiente scan lo pisaría.
--   - El admin puede tener override de poster pero NO de backdrop (o
--     viceversa); con dos columnas haría falta lógica de "preserva
--     esto sí, este otro no" en el upsert del scanner.
--   - Patrón ya establecido para channel_logo_overrides — consistente.
--
-- Read flow:
--   1. CollectionHandler.Get carga la row de `collections` (TMDb data).
--   2. Aplica los overrides por encima:
--        override.file no vacío  → URL local servida desde disco
--        override.url no vacío   → URL externa pegada por el admin
--        nada                    → la URL de TMDb que la row trae
--   3. Si la colección no tiene override, comportamiento idéntico al
--      pre-migration: cero coste para sagas no editadas.
--
-- Archivos subidos viven bajo <imageDir>/collection-images/<basename>.
-- El basename incluye collection_id + timestamp para que un nuevo
-- upload bumpee el cache del browser sin tocar la URL del proxy.
CREATE TABLE collection_image_overrides (
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    image_type    TEXT NOT NULL CHECK (image_type IN ('poster', 'backdrop')),
    url           TEXT NOT NULL DEFAULT '',
    file          TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (collection_id, image_type),
    CHECK (url <> '' OR file <> '')
);
