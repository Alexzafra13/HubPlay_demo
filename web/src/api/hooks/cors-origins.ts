// CORS origins hooks (PR4 feature — panel admin de orígenes
// dinámicos). Sólo se invocan desde la sección owner-only de
// /admin/system; un admin sin can_view_audit tampoco lo necesita
// (el backend devolverá 403, así que el hook caería en error
// gracioso).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { CorsOriginsListResponse } from "../types";

export function useCorsOrigins(enabled = true) {
  return useQuery<CorsOriginsListResponse>({
    queryKey: queryKeys.corsOrigins,
    queryFn: () => api.listCorsOrigins(),
    enabled,
    // staleTime 0 — el cambio raro pero crítico. Forzar fresh fetch
    // al abrir el panel evita que un Add reciente hecho desde otra
    // pestaña no se vea.
    staleTime: 0,
  });
}

export function useAddCorsOrigin() {
  const qc = useQueryClient();
  return useMutation<
    CorsOriginsListResponse,
    Error,
    { origin: string; note: string }
  >({
    mutationFn: ({ origin, note }) => api.addCorsOrigin(origin, note),
    onSuccess: (data) => {
      // El backend ya devuelve el estado nuevo — lo metemos directo
      // al cache sin un GET extra.
      qc.setQueryData(queryKeys.corsOrigins, data);
    },
  });
}

export function useDeleteCorsOrigin() {
  const qc = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (origin) => api.deleteCorsOrigin(origin),
    onSuccess: () => {
      // DELETE devuelve 204 sin body — invalidamos para re-pedir
      // el listado y que la fila desaparezca.
      qc.invalidateQueries({ queryKey: queryKeys.corsOrigins });
    },
  });
}
