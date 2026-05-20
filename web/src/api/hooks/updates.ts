// Update notifier hooks (PR2 update-notifier).
//
// El backend mantiene el snapshot del último poll a GitHub Releases en
// memoria; la query es lectura barata (sin IO). Hacemos refetch suave
// (5 min) más que para "ver cambios" porque el snapshot del backend
// puede haber rotado tras un check del ticker — pero no obsesionamos
// con keep-fresh, la noti es de cadencia diaria, no en tiempo real.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { UpdateStatus, UpdatesConfig } from "../types";

export function useUpdateStatus(enabled = true) {
  return useQuery<UpdateStatus>({
    queryKey: queryKeys.updateStatus,
    queryFn: () => api.getUpdateStatus(),
    enabled,
    // 5 min — el backend chequea cada 24h; resincronizar el cache del
    // cliente más frecuentemente es derroche. Lo bueno: si el operador
    // navega activamente al panel admin la nota actual está siempre
    // pintada sin esperar.
    staleTime: 5 * 60_000,
    // Si el endpoint no está disponible (dev build, deps.Updates=nil)
    // devuelve 404; no spamees con retries.
    retry: false,
  });
}

/**
 * Mutation para forzar check manual. Backend rate-limita a 1/min — si
 * el usuario clicka antes, devuelve 429 (apareceá como error.message
 * en la mutation, el UI muestra "Espera un minuto antes de reintentar").
 */
export function useCheckUpdatesNow() {
  const qc = useQueryClient();
  return useMutation<UpdateStatus, Error>({
    mutationFn: () => api.checkUpdates(),
    onSuccess: (fresh) => {
      // Pisamos la cache con la respuesta — evita un round-trip
      // adicional para refrescar el badge tras el check manual.
      qc.setQueryData(queryKeys.updateStatus, fresh);
    },
  });
}

/**
 * Estado del toggle runtime del admin. Lectura barata — el handler lee
 * de memoria. Sin polling agresivo, igual que useUpdateStatus.
 */
export function useUpdatesConfig(enabled = true) {
  return useQuery<UpdatesConfig>({
    queryKey: queryKeys.updatesConfig,
    queryFn: () => api.getUpdatesConfig(),
    enabled,
    staleTime: 5 * 60_000,
    retry: false,
  });
}

/**
 * Mutation para cambiar el toggle. Persiste server-side y, en
 * onSuccess, invalida `updateStatus` para que el banner refleje el
 * nuevo estado sin esperar al staleTime.
 */
export function useSetUpdatesConfig() {
  const qc = useQueryClient();
  return useMutation<UpdatesConfig, Error, boolean>({
    mutationFn: (enabled: boolean) => api.setUpdatesConfig(enabled),
    onSuccess: (fresh) => {
      qc.setQueryData(queryKeys.updatesConfig, fresh);
      void qc.invalidateQueries({ queryKey: queryKeys.updateStatus });
    },
  });
}
