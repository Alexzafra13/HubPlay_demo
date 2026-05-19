// Audit log hooks (PR5-C — UI del panel).
//
// El audit log es read-mostly: el usuario hace queries con filtros
// (tipo, actor, ventana temporal, búsqueda) y los resultados se
// caché por la combinación de filtros.  No usamos staleTime largo
// porque la tabla crece en vivo y queremos refresh al volver al
// panel; pero tampoco invalidamos en cada keystroke — los filtros
// se aplican on-submit (botón "Aplicar" o Enter), no en cada cambio.

import { useQuery } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { AuditLogQueryResponse } from "../types";

export interface AuditLogFilters {
  type?: string;
  actor?: string;
  from?: string;
  to?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export function useAuditLog(filters: AuditLogFilters, enabled = true) {
  return useQuery<AuditLogQueryResponse>({
    queryKey: queryKeys.auditLog(filters as Record<string, unknown>),
    queryFn: () => api.queryAuditLog(filters),
    enabled,
    // 15s — corto para que el panel sea live-feeling pero suficiente
    // para que el usuario que pagina rápido no haga un round-trip
    // por cada click.
    staleTime: 15_000,
  });
}

export function useAuditEventTypes(enabled = true) {
  return useQuery<string[]>({
    queryKey: queryKeys.auditEventTypes,
    queryFn: () => api.listAuditEventTypes(),
    enabled,
    // 5 min — la lista de tipos cambia con cada nuevo productor que
    // se cablea, no en runtime normal. Cachear largo es lo correcto.
    staleTime: 5 * 60_000,
  });
}
