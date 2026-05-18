-- +goose Up
-- Per-item metadata lock — opt-out from automatic provider refreshes.
--
-- When a row exists for an item, the scanner's enrichMetadata,
-- enrichIfMissing y RefreshMetadata se saltan ese item. La presencia
-- de la fila es la señal — no hay payload, sólo el item_id y un
-- timestamp para que el admin pueda ver en qué orden se editaron.
--
-- Por qué tabla separada en vez de columna en `items`:
--
--   - Cero cambios en items.Update y la maquinaria sqlc que la
--     respalda. Añadir una columna implicaría regenerar sqlc + tocar
--     cada query que escribe en items (todas las funciones del
--     scanner) para preservar el valor a través de Update; un olvido
--     silenciosamente desbloquearía el item.
--   - Patrón ya establecido (channel_overrides, library_channel_order):
--     el estado de "el admin ha tocado esto" vive en su tabla.
--   - GC trivial: si un item se borra, el FK ON DELETE CASCADE limpia
--     la lock asociada automáticamente.
--
-- Set: el endpoint Identify (POST /items/{id}/identify) inserta tras
-- aplicar el match. El editor manual de metadatos (PATCH /items/{id}/
-- metadata) también, así que cualquier edición humana sobrevive al
-- siguiente "Refresh metadata" global.
--
-- Unset: el admin puede desbloquear desde la UI (kebab del detalle),
-- lo que dispara un DELETE de la fila — la próxima vez que pase el
-- scanner volverá a auto-enriquecer.
CREATE TABLE item_metadata_locks (
    item_id    TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    locked_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
