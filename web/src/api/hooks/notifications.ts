// Notifications inbox por usuario. Backend: internal/notification +
// /me/notifications. Push en vivo via SSE de /me/events filtrando
// notification.created.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useUserEventStream } from "@/hooks/useUserEventStream";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { NotificationsResponse } from "../types";

// useMyNotifications devuelve {data, unread_count}. Suscribe en
// segundo plano al SSE `notification.created` para invalidar
// inmediatamente cuando llegue una notif (badge se actualiza en
// vivo, sin esperar al siguiente refetch).
//
// staleTime es bajo porque incluso si el SSE se cae, queremos que
// el dropdown muestre la verdad sin tener que hacer click manual
// en "actualizar".
export function useMyNotifications(enabled = true) {
  const queryClient = useQueryClient();

  // Suscripcion SSE: cuando el backend publica notification.created
  // para nuestro user, invalidamos la query y triggers refetch.
  useUserEventStream(
    "notification.created",
    () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.myNotifications });
    },
    enabled,
  );

  return useQuery<NotificationsResponse>({
    queryKey: queryKeys.myNotifications,
    queryFn: () => api.listMyNotifications(),
    enabled,
    staleTime: 30 * 1000,
  });
}

export function useMarkNotificationRead() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.markNotificationRead(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.myNotifications });
    },
  });
}

export function useMarkAllNotificationsRead() {
  const queryClient = useQueryClient();
  return useMutation<{ marked_count: number }, Error, void>({
    mutationFn: () => api.markAllNotificationsRead(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.myNotifications });
    },
  });
}
