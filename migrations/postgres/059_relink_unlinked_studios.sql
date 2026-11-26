-- +goose Up
--
-- 059_relink_unlinked_studios.sql — vuelve a enlazar items con estudio
-- (metadata.studio) que se quedaron sin metadata.studio_id.
--
-- Cómo pasaba: "Actualizar metadatos" / Identificar / refresco de
-- biblioteca borran la fila de metadata (y su studio_id) y el scanner
-- llamaba a EnsureStudio con tmdb_id. Si existía la fila que creó el
-- backfill de 032 (mismo slug, sin tmdb_id), el INSERT chocaba por
-- id/slug — el ON CONFLICT solo cubría tmdb_id —, EnsureStudio fallaba y
-- el item quedaba suelto: la ficha enlazaba a /studios/<slug> y la página
-- salía con 0 títulos ("Lucasfilm Ltd."). EnsureStudio ya adopta esas
-- filas; esto repara lo que quedó roto, con la misma receta de slug que
-- el backfill de 032 (lo exótico lo arregla el próximo refresco).
UPDATE metadata
SET studio_id = (
    SELECT s.id FROM studios s
    WHERE s.slug = lower(
        replace(replace(replace(replace(replace(replace(replace(replace(
            trim(metadata.studio),
            ' ', '-'), '.', ''), ',', ''), '&', 'and'),
            '/', '-'), '(', ''), ')', ''), '''', '')
    )
)
WHERE studio_id IS NULL
  AND COALESCE(metadata.studio, '') <> '';
