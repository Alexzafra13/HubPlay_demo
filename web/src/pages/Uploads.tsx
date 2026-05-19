// Página /uploads — frontend de la feature PR2 (subida de media).
//
// Layout:
//   ┌────────────────────────────────────────────────────────────────┐
//   │ Drop zone + library picker                                     │
//   │  · drag & drop o file picker                                   │
//   │  · selector de librería destino (movies/shows del usuario)     │
//   │  · botón "Subir" inicia tus-js                                 │
//   ├────────────────────────────────────────────────────────────────┤
//   │ Activos — lista de uploads en vuelo                            │
//   │  · barra de progreso 0..100% durante bytes                     │
//   │  · phase label tras 100% (validating/probing/moving/indexing)  │
//   │  · botón cancelar                                              │
//   ├────────────────────────────────────────────────────────────────┤
//   │ Historial — audit log del usuario (últimos 50)                 │
//   │  · accepted, rejected, aborted, error                          │
//   │  · final_path + mime + duración + bytes                        │
//   └────────────────────────────────────────────────────────────────┘
//
// State machine de cada ActiveUpload:
//
//   queued (file en cola, sin POST aún)
//     │
//     ▼  user clicks Subir → tus-js POST de creación
//   uploading (progress 0..1 desde tus-js onProgress)
//     │
//     ▼  100% bytes enviados, tusd dispara CompleteUploads → service.Finish
//   validating → probing → moving → indexing
//     │  (todas vía SSE upload.phase, server-pushed)
//     ▼
//   done (SSE upload.done) | error (SSE upload.error / abort cliente)
//
// Cuando done/error: el upload "se gradúa" — se quita del panel
// Activos y se invalida la query del Historial para que aparezca.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import * as tus from "tus-js-client";
import {
  Upload as UploadIcon,
  X,
  CheckCircle2,
  XCircle,
  Loader2,
  FileVideo,
} from "lucide-react";

import { FolderBrowser } from "@/components/uploads/FolderBrowser";
import { api } from "@/api/client";
import {
  useMe,
  useLibraries,
  useMyUploads,
  useUploadEvents,
  type ActiveUpload,
  type UploadEvent,
  phaseToStatus,
} from "@/api/hooks";
import type { Library, UploadAuditEntry } from "@/api/types";
import {
  Button,
  EmptyState,
  PageHeader,
  ProgressBar,
} from "@/components/common";

// Extensiones aceptadas — espejo cliente-side del whitelist del backend.
// Aquí sólo gatea el file picker / drag-drop antes de iniciar tus, para
// que el usuario no espere a un 403 del servidor. La fuente de verdad
// es el backend (internal/upload/validator.go).
const ACCEPTED_EXTENSIONS = [
  ".mkv", ".mp4", ".m4v", ".mov", ".avi", ".webm",
  ".ts", ".vob", ".mpg", ".mpeg",
  ".srt", ".ass", ".vtt",
];

export default function Uploads() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const { data: libraries } = useLibraries();
  const { data: history = [] } = useMyUploads(50, !!me?.can_upload);
  const queryClient = useQueryClient();

  const [active, setActive] = useState<ActiveUpload[]>([]);

  // Librerías elegibles: tipos movies/shows a los que el usuario tiene
  // acceso. Las libtv / sin paths quedan fuera (el backend rechazaría
  // igualmente). La lista la usa el picker; si está vacía, el dropzone
  // muestra empty-state.
  const targetableLibraries = useMemo(
    () =>
      (libraries ?? []).filter(
        (lib) => lib.content_type === "movies" || lib.content_type === "shows",
      ),
    [libraries],
  );

  // SSE handler: traduce events a mutaciones del array de activos.
  // El correlacionador es serverID (asignado al obtener Location del
  // POST tus). Si llega un evento con id desconocido lo ignoramos
  // silencioso — puede ser un upload de otra pestaña del mismo
  // usuario (el filtro por user_id ya lo hace el server, pero
  // pestañas distintas no comparten state).
  const onUploadEvent = useCallback(
    (evt: UploadEvent) => {
      setActive((prev) => {
        const idx = prev.findIndex((u) => u.serverID === evt.data.id);
        if (idx < 0) return prev;
        const next = [...prev];
        const cur = { ...next[idx] };
        if (evt.type === "upload.phase" && evt.data.phase) {
          cur.status = phaseToStatus(evt.data.phase);
        } else if (evt.type === "upload.done") {
          cur.status = "done";
        } else if (evt.type === "upload.error") {
          cur.status = "error";
          cur.errorMessage = evt.data.reason ?? "";
        }
        next[idx] = cur;
        return next;
      });
      // Terminal events: invalida historial para que aparezca la fila
      // y arranca un timer para limpiar el upload del panel Activos.
      if (evt.type === "upload.done" || evt.type === "upload.error") {
        queryClient.invalidateQueries({ queryKey: ["uploads"] });
        // Mantenemos el item en el panel 3s para que el usuario vea
        // el estado final antes de que desaparezca.
        const targetID = evt.data.id;
        setTimeout(() => {
          setActive((prev) => prev.filter((u) => u.serverID !== targetID));
        }, 3000);
      }
    },
    [queryClient],
  );

  useUploadEvents(onUploadEvent, !!me?.can_upload);

  // Cleanup en unmount: abortar uploads activos para no dejar bytes
  // huérfanos en el staging. La pipeline.Aborted del server libera
  // la cuota y borra el blob.
  useEffect(() => {
    return () => {
      for (const u of active) {
        if (u.tusUpload && (u.status === "uploading" || u.status === "queued")) {
          try {
            u.tusUpload.abort(true);
          } catch {
            // ignore — best-effort cleanup
          }
        }
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // mount/unmount only

  function startUpload(file: File, libraryID: string, subpath: string) {
    const localID = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const initial: ActiveUpload = {
      localID,
      serverID: null,
      filename: file.name,
      size: file.size,
      libraryID,
      status: "queued",
      progress: 0,
    };
    setActive((prev) => [...prev, initial]);

    const upload = new tus.Upload(file, {
      endpoint: api.uploadsEndpoint(),
      // Auth via cookie hubplay_access — los XHR de tus-js-client
      // envían cookies automáticamente en same-origin (caso producción:
      // el SPA y el API viven en el mismo binario Go). Para deploys
      // tras proxy con CORS habría que añadir un onBeforeRequest que
      // ponga `req.setRequestCredentials("include")`; cuando llegue
      // ese deploy lo cableamos.
      //
      // chunkSize 8 MiB es el sweet spot: el server escribe en buffer
      // interno de 1 MiB, chunks demasiado grandes ocupan memoria,
      // demasiado pequeños hacen muchas PATCH requests.
      chunkSize: 8 * 1024 * 1024,
      // retryDelays opt-in: tus reintenta automáticamente cuando la
      // red parpadea, lo cual es exactamente lo que queremos para un
      // upload grande sobre una conexión casera.
      retryDelays: [0, 1000, 3000, 5000, 10000],
      metadata: {
        filename: file.name,
        filetype: file.type || "application/octet-stream",
        library_id: libraryID,
        // Subpath dentro de la librería (PR6 file explorer). Vacío =
        // raíz, mismo comportamiento pre-PR6. tusd lo persiste en su
        // .info y el pipeline post-bytes lo lee desde ahí.
        subpath: subpath,
      },
      onError: (err) => {
        setActive((prev) =>
          prev.map((u) =>
            u.localID === localID
              ? { ...u, status: "error", errorMessage: err.message }
              : u,
          ),
        );
      },
      onProgress: (bytesSent, bytesTotal) => {
        const pct = bytesTotal > 0 ? bytesSent / bytesTotal : 0;
        setActive((prev) =>
          prev.map((u) =>
            u.localID === localID
              ? { ...u, status: "uploading", progress: pct }
              : u,
          ),
        );
      },
      onAfterResponse: (req, res) => {
        // tras el POST de creación, tus expone upload.url; capturamos
        // el id (último segmento) para correlacionar SSE.
        if (req.getMethod() === "POST" && res.getStatus() === 201) {
          const loc = res.getHeader("Location") ?? "";
          // Location: /api/v1/uploads/<id>
          const id = loc.split("/").filter(Boolean).pop() ?? null;
          setActive((prev) =>
            prev.map((u) =>
              u.localID === localID ? { ...u, serverID: id } : u,
            ),
          );
        }
      },
      onSuccess: () => {
        // No marcamos done aquí — la fase final llega vía SSE cuando
        // la pipeline del backend termina. tus.onSuccess sólo dice
        // "los bytes están en disco del server"; queda validate/probe/
        // move/index por hacer.
      },
    });

    setActive((prev) =>
      prev.map((u) => (u.localID === localID ? { ...u, tusUpload: upload } : u)),
    );
    upload.start();
  }

  function cancelUpload(localID: string) {
    setActive((prev) => {
      const u = prev.find((x) => x.localID === localID);
      if (u?.tusUpload) {
        try {
          u.tusUpload.abort(true);
        } catch {
          // ignore
        }
      }
      return prev.filter((x) => x.localID !== localID);
    });
  }

  if (!me) return null;
  if (!me.can_upload) {
    return (
      <div className="px-4 py-8 sm:px-10">
        <EmptyState
          title={t("uploads.notAllowedTitle")}
          description={t("uploads.notAllowedDesc")}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 px-4 py-6 sm:px-10 sm:py-8">
      <PageHeader title={t("uploads.title")} subtitle={t("uploads.subtitle")} />

      <UploadDropzone
        libraries={targetableLibraries}
        quotaUsed={me.upload_used_bytes}
        quotaTotal={me.upload_quota_bytes}
        onUpload={startUpload}
      />

      {active.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-base font-semibold text-text-primary">
            {t("uploads.activeTitle")}
          </h2>
          <ul className="flex flex-col gap-2">
            {active.map((u) => (
              <ActiveUploadRow
                key={u.localID}
                upload={u}
                onCancel={() => cancelUpload(u.localID)}
              />
            ))}
          </ul>
        </section>
      )}

      <section className="flex flex-col gap-3">
        <h2 className="text-base font-semibold text-text-primary">
          {t("uploads.historyTitle")}
        </h2>
        <UploadHistoryList entries={history} />
      </section>
    </div>
  );
}

// ─── UploadDropzone ──────────────────────────────────────────────────

interface DropzoneProps {
  libraries: Library[];
  quotaUsed?: number;
  quotaTotal?: number;
  onUpload: (file: File, libraryID: string, subpath: string) => void;
}

function UploadDropzone({
  libraries,
  quotaUsed,
  quotaTotal,
  onUpload,
}: DropzoneProps) {
  const { t } = useTranslation();
  const [dragging, setDragging] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [libraryID, setLibraryID] = useState<string>(
    libraries[0]?.id ?? "",
  );
  // Subpath dentro de la librería elegida (PR6 file explorer). Vacío
  // = raíz. El FolderBrowser lo actualiza cuando el usuario navega.
  const [subpath, setSubpath] = useState<string>("");
  const [rejectReason, setRejectReason] = useState<string | null>(null);

  // Mantén el library seleccionado coherente con la lista (puede
  // cambiar si el admin crea/borra librerías mientras la página está
  // abierta).
  useEffect(() => {
    if (libraryID && libraries.find((l) => l.id === libraryID)) return;
    setLibraryID(libraries[0]?.id ?? "");
  }, [libraries, libraryID]);

  // validateFiles aplica las dos reglas cliente-side (extensión +
  // cuota) y devuelve la slice de aceptados.  Side-effect: pone
  // rejectReason si alguno falla — el caller decide si seguir.
  // Separado de validateAndStage para poder reutilizarlo desde el
  // drop-on-folder (que valida y sube DIRECTO, sin pasar por la cola).
  function validateFiles(incoming: FileList | File[]): File[] {
    setRejectReason(null);
    const arr = Array.from(incoming);
    const valid: File[] = [];
    for (const f of arr) {
      const dot = f.name.lastIndexOf(".");
      const ext = dot >= 0 ? f.name.slice(dot).toLowerCase() : "";
      if (!ACCEPTED_EXTENSIONS.includes(ext)) {
        setRejectReason(
          t("uploads.rejectExtension", { name: f.name, ext: ext || "—" }),
        );
        continue;
      }
      if (quotaTotal && quotaTotal > 0) {
        const headroom = quotaTotal - (quotaUsed ?? 0);
        if (f.size > headroom) {
          setRejectReason(t("uploads.rejectQuota", { name: f.name }));
          continue;
        }
      }
      valid.push(f);
    }
    return valid;
  }

  function validateAndStage(incoming: FileList | File[]) {
    const valid = validateFiles(incoming);
    setFiles((prev) => [...prev, ...valid]);
  }

  // Drop directo sobre una carpeta del FolderBrowser (Termius-style):
  // los ficheros válidos se suben INMEDIATAMENTE a esa carpeta sin
  // pasar por la cola visible. El subpath puede ser distinto del
  // path actual del browser (si arrastras a "Drama" estando en
  // "Movies/", aterriza en "Drama" directo). Si algún fichero falla
  // la validación, los demás suben igual y el rejectReason se ve.
  function handleDropOnFolder(incoming: File[], targetSubpath: string) {
    if (!libraryID) return;
    const valid = validateFiles(incoming);
    for (const f of valid) {
      onUpload(f, libraryID, targetSubpath);
    }
  }

  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault();
    setDragging(false);
    if (e.dataTransfer.files?.length) {
      validateAndStage(e.dataTransfer.files);
    }
  }

  function onPickerChange(e: React.ChangeEvent<HTMLInputElement>) {
    if (e.target.files?.length) {
      validateAndStage(e.target.files);
    }
    // Permite seleccionar el mismo fichero de nuevo tras retirarlo de
    // la cola: reset del input.
    e.target.value = "";
  }

  function removeFromQueue(index: number) {
    setFiles((prev) => prev.filter((_, i) => i !== index));
  }

  function startAll() {
    if (!libraryID) return;
    for (const f of files) {
      onUpload(f, libraryID, subpath);
    }
    setFiles([]);
  }

  if (libraries.length === 0) {
    return (
      <EmptyState
        title={t("uploads.noLibrariesTitle")}
        description={t("uploads.noLibrariesDesc")}
      />
    );
  }

  return (
    <section className="flex flex-col gap-3">
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={[
          "flex flex-col items-center justify-center gap-3 rounded-[--radius-lg] border-2 border-dashed p-8 transition-colors text-center",
          dragging
            ? "border-accent bg-accent/10"
            : "border-border bg-bg-elevated",
        ].join(" ")}
      >
        <UploadIcon size={32} className="text-text-muted" aria-hidden />
        <p className="text-sm text-text-secondary">
          {t("uploads.dropzoneHint")}
        </p>
        <label className="cursor-pointer text-sm font-medium text-accent hover:underline">
          {t("uploads.pickerCta")}
          <input
            type="file"
            multiple
            className="hidden"
            accept={ACCEPTED_EXTENSIONS.join(",")}
            onChange={onPickerChange}
          />
        </label>
        <p className="text-xs text-text-muted/70">
          {t("uploads.dropzoneSecondaryHint", {
            defaultValue:
              "O arrastra los ficheros directamente sobre una carpeta del explorador de abajo para subir ahí.",
          })}
        </p>
      </div>

      {rejectReason && (
        <p className="text-sm text-red-400" role="alert">
          {rejectReason}
        </p>
      )}

      {/* Folder browser SIEMPRE visible — el drop-on-folder es el
          flujo principal Termius-style, no requiere encolar antes.
          La cola visible aparece SOLO si el usuario suelta en el
          dropzone general (que stage en lugar de subir directo). */}
      <FolderBrowser
        libraries={libraries}
        libraryID={libraryID}
        path={subpath}
        onChange={(libID, p) => {
          setLibraryID(libID);
          setSubpath(p);
        }}
        onDropFiles={handleDropOnFolder}
      />

      {files.length > 0 && (
        <div className="flex flex-col gap-3 rounded-[--radius-md] border border-border bg-bg-elevated p-4">
          <ul className="flex flex-col gap-1">
            {files.map((f, i) => (
              <li
                key={`${f.name}-${i}`}
                className="flex items-center justify-between gap-3 text-sm"
              >
                <span className="flex items-center gap-2 truncate">
                  <FileVideo size={14} className="text-text-muted shrink-0" aria-hidden />
                  <span className="truncate">{f.name}</span>
                  <span className="text-text-muted shrink-0">
                    {humanBytes(f.size)}
                  </span>
                </span>
                <button
                  type="button"
                  onClick={() => removeFromQueue(i)}
                  aria-label={t("common.remove", { defaultValue: "Eliminar" })}
                  className="text-text-muted hover:text-text-primary"
                >
                  <X size={14} aria-hidden />
                </button>
              </li>
            ))}
          </ul>

          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="text-text-muted">
              {subpath ? (
                <>
                  {t("uploads.targetHere", { defaultValue: "Subir a" })}{" "}
                  <span className="font-mono text-text-secondary">/{subpath}</span>
                </>
              ) : (
                t("uploads.targetRoot", {
                  defaultValue: "Subir a la raíz de la biblioteca",
                })
              )}
            </span>
            <Button onClick={startAll} disabled={!libraryID || files.length === 0}>
              {t("uploads.startCta", { count: files.length })}
            </Button>
          </div>
        </div>
      )}

      {quotaTotal !== undefined && quotaTotal > 0 && (
        <p className="text-xs text-text-muted">
          {t("uploads.quotaLine", {
            used: humanBytes(quotaUsed ?? 0),
            total: humanBytes(quotaTotal),
          })}
        </p>
      )}
    </section>
  );
}

// ─── ActiveUploadRow ─────────────────────────────────────────────────

function ActiveUploadRow({
  upload,
  onCancel,
}: {
  upload: ActiveUpload;
  onCancel: () => void;
}) {
  const { t } = useTranslation();

  // Mensajes localizados según status.
  const statusLabel = (() => {
    switch (upload.status) {
      case "queued":
        return t("uploads.statusQueued");
      case "uploading":
        return t("uploads.statusUploading");
      case "validating":
        return t("uploads.statusValidating");
      case "probing":
        return t("uploads.statusProbing");
      case "moving":
        return t("uploads.statusMoving");
      case "indexing":
        return t("uploads.statusIndexing");
      case "done":
        return t("uploads.statusDone");
      case "error":
        return t("uploads.statusError");
    }
  })();

  return (
    <li className="flex flex-col gap-2 rounded-[--radius-md] border border-border bg-bg-elevated p-3">
      <div className="flex items-center justify-between gap-3">
        <span className="flex items-center gap-2 truncate text-sm">
          {upload.status === "done" ? (
            <CheckCircle2 size={14} className="text-green-500 shrink-0" aria-hidden />
          ) : upload.status === "error" ? (
            <XCircle size={14} className="text-red-500 shrink-0" aria-hidden />
          ) : upload.status === "uploading" || upload.status === "queued" ? (
            <FileVideo size={14} className="text-text-muted shrink-0" aria-hidden />
          ) : (
            <Loader2 size={14} className="text-accent shrink-0 animate-spin" aria-hidden />
          )}
          <span className="truncate">{upload.filename}</span>
        </span>
        {(upload.status === "uploading" || upload.status === "queued") && (
          <button
            type="button"
            onClick={onCancel}
            aria-label={t("common.cancel")}
            className="text-text-muted hover:text-text-primary"
          >
            <X size={14} aria-hidden />
          </button>
        )}
      </div>

      {upload.status === "uploading" && (
        <ProgressBar value={upload.progress * 100} />
      )}

      <div className="flex items-center justify-between text-xs text-text-muted">
        <span>{statusLabel}</span>
        <span>{humanBytes(upload.size)}</span>
      </div>

      {upload.status === "error" && upload.errorMessage && (
        <p className="text-xs text-red-400" role="alert">
          {upload.errorMessage}
        </p>
      )}
    </li>
  );
}

// ─── UploadHistoryList ───────────────────────────────────────────────

function UploadHistoryList({ entries }: { entries: UploadAuditEntry[] }) {
  const { t } = useTranslation();

  if (entries.length === 0) {
    return (
      <EmptyState
        title={t("uploads.historyEmptyTitle")}
        description={t("uploads.historyEmptyDesc")}
      />
    );
  }

  return (
    <ul className="flex flex-col gap-2">
      {entries.map((e) => (
        <li
          key={e.id}
          className="flex flex-col gap-1 rounded-[--radius-md] border border-border bg-bg-elevated p-3 sm:flex-row sm:items-center sm:gap-4"
        >
          <span className="flex items-center gap-2 truncate text-sm">
            <OutcomeIcon outcome={e.outcome} />
            <span className="truncate">{e.original_name}</span>
          </span>
          <span className="text-xs text-text-muted">
            {humanBytes(e.bytes)}
          </span>
          <span className="text-xs text-text-muted">
            {new Date(e.started_at).toLocaleString()}
          </span>
          <span className="text-xs text-text-muted sm:ml-auto">
            {t(`uploads.outcome_${e.outcome}`)}
          </span>
          {e.error_message && (
            <span className="text-xs text-red-400 sm:basis-full">
              {e.error_message}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

function OutcomeIcon({
  outcome,
}: {
  outcome: UploadAuditEntry["outcome"];
}) {
  if (outcome === "accepted") {
    return (
      <CheckCircle2
        size={14}
        className="text-green-500 shrink-0"
        aria-hidden
      />
    );
  }
  return <XCircle size={14} className="text-red-500 shrink-0" aria-hidden />;
}

// ─── helpers ─────────────────────────────────────────────────────────

// humanBytes formatea bytes a "X.Y MiB" / "X.Y GiB". Suficiente para
// la página de uploads donde el rango va de KB a decenas de GB; no
// usamos una lib porque la conversión binaria es 4 líneas y añadir
// "filesize" sólo para esto era overkill.
function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let val = n / 1024;
  let i = 0;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return `${val.toFixed(1)} ${units[i]}`;
}
