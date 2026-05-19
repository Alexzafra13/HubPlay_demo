// Upload hooks (frontend de la feature PR2).
//
// Dos surfaces:
//   useMyUploads()      — snapshot inicial del audit log del usuario,
//                          via GET /api/v1/uploads/mine. Cacheado por
//                          TanStack Query con staleTime corto porque
//                          la página de uploads es 99% live SSE; el
//                          query existe para repoblar al abrir.
//   useUploadEvents()   — suscripción al SSE /api/v1/uploads/events.
//                          Multiplexa via eventBus para que pestañas
//                          que abren la página dos veces no encadenen
//                          dos EventSources.
//
// El SSE entrega events tipados:
//   upload.phase  { id, user_id, phase }    — validating/probing/moving/indexing
//   upload.done   { id, user_id, library_id, final_path }
//   upload.error  { id, user_id, reason }
//   upload.bytes  { id, user_id, offset, total } — reservado para
//                                                   futura sincronización
//                                                   cross-device; v1
//                                                   no la emite todavía.

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import { subscribeSse } from "@/hooks/eventBus";
import type { UploadAuditEntry, UploadPhase } from "../types";

export function useMyUploads(limit = 50, enabled = true) {
  return useQuery<UploadAuditEntry[]>({
    queryKey: queryKeys.myUploads(limit),
    queryFn: () => api.listMyUploads(limit),
    enabled,
    // El audit es histórico — no refetch en focus. Los uploads en vuelo
    // ya se reflejan en el panel "Activos" via SSE; este query alimenta
    // el panel "Historial". 60s de staleTime es generoso pero seguro.
    staleTime: 60_000,
  });
}

// ─── SSE stream ──────────────────────────────────────────────────────

/** El payload tipado de cada evento upload.* emitido por el server. */
export interface UploadEventData {
  id: string;
  user_id?: string;
  phase?: UploadPhase;
  library_id?: string;
  final_path?: string;
  reason?: string;
  offset?: number;
  total?: number;
}

export interface UploadEvent {
  type: "upload.phase" | "upload.done" | "upload.error" | "upload.bytes";
  data: UploadEventData;
}

/**
 * useUploadEvents — subscribe al SSE de uploads. El callback recibe
 * cada evento parseado; el componente que llama es responsable de
 * agregarlos al estado de su lista (mapa id → estado en curso).
 *
 * Se monta sólo si `enabled` es true (la página suele pasar
 * `me?.can_upload` para que un usuario sin permiso no abra socket
 * inútil).
 *
 * Reusa el eventBus multiplexado del proyecto — si dos componentes
 * se suscriben simultáneamente, hay UNA EventSource con dos handlers.
 */
export function useUploadEvents(
  onEvent: (evt: UploadEvent) => void,
  enabled = true,
) {
  // Guardamos el callback en ref para que cambiar su identidad no
  // desencadene un re-subscribe (el componente padre pasa una arrow
  // function que cambia cada render).
  const handlerRef = useRef(onEvent);
  useEffect(() => {
    handlerRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!enabled) return;
    const types: UploadEvent["type"][] = [
      "upload.phase",
      "upload.done",
      "upload.error",
      "upload.bytes",
    ];
    const unsubs = types.map((t) =>
      subscribeSse("/api/v1/uploads/events", true, t, (raw) => {
        try {
          const parsed = JSON.parse(raw) as UploadEvent;
          handlerRef.current(parsed);
        } catch {
          // Trama malformada — la dejamos pasar silenciosa. El backend
          // sólo emite JSON válido; un parse-fail aquí señala bug del
          // dispatcher SSE upstream, no del cliente.
        }
      }),
    );
    return () => {
      for (const u of unsubs) u();
    };
  }, [enabled]);
}

// ─── ActiveUpload local state ────────────────────────────────────────

/**
 * Estado in-memory del upload mientras los bytes están en vuelo. Vive
 * en el componente padre (Uploads page), no en TanStack Query, porque
 * tus-js-client mantiene el Upload object propio y el ciclo de vida
 * no encaja con el modelo de cache de queries.
 *
 * Las fases (validating/probing/etc.) llegan POST-bytes via SSE y
 * mutan este estado. Cuando llega upload.done o upload.error el
 * upload "se gradúa": se quita de la lista de activos y se invalida
 * la query de audit para que el historial lo muestre.
 */
export interface ActiveUpload {
  // ID local generado en el cliente — los bytes-in-flight aún no
  // tienen un upload_id del servidor (se asigna en el POST de
  // creación tus). Una vez ese POST responde, guardamos también el
  // serverID para correlacionar SSE.
  localID: string;
  serverID: string | null;
  filename: string;
  size: number;
  libraryID: string | null;
  // Estados de cliente — orden temporal:
  //   queued       → tus aún no ha hecho el POST de creación
  //   uploading    → bytes en vuelo, progress es el % conocido por tus
  //   validating   → SSE phase=validating
  //   probing      → SSE phase=probing
  //   moving       → SSE phase=moving
  //   indexing     → SSE phase=indexing
  //   done         → SSE upload.done
  //   error        → SSE upload.error o fallo cliente-side (red, abort)
  status:
    | "queued"
    | "uploading"
    | "validating"
    | "probing"
    | "moving"
    | "indexing"
    | "done"
    | "error";
  // 0..1 — sólo significativo en status=uploading. Las fases post-bytes
  // no tienen porcentaje (son operaciones discretas), las renderizamos
  // como un spinner + nombre de fase.
  progress: number;
  errorMessage?: string;
  // Referencia al Upload object de tus-js para poder abort() / start().
  // any para evitar acoplar este module al tipo de tus-js (que es
  // pesado de importar a archivos que sólo necesitan el shape).
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  tusUpload?: any;
}

/** Mapa de fases SSE a status de la UI. Helper para que el componente
 *  no se ate al string literal del backend. */
export function phaseToStatus(p: UploadPhase): ActiveUpload["status"] {
  switch (p) {
    case "validating":
      return "validating";
    case "probing":
      return "probing";
    case "moving":
      return "moving";
    case "indexing":
      return "indexing";
  }
}
