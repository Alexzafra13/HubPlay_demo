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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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

// Set para lookups O(1) en validateFiles() que recorre cada fichero
// del drop. El array de arriba se conserva para el `accept=` del input
// (el atributo nativo necesita la lista comma-separated).
const ACCEPTED_EXTENSIONS_SET = new Set(ACCEPTED_EXTENSIONS);

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
  //
  // Patrón "latest value via ref": el unmount cleanup necesita leer
  // el estado actualizado de `active`. Añadirlo a deps re-ejecutaría
  // el cleanup en cada cambio (abortando uploads activos cada vez
  // que cambian estado). El ref se actualiza en un effect dedicado
  // y el cleanup lee `.current` al disparar, garantizando snapshot
  // fresco — todo dentro de effects, satisfaciendo react-hooks/refs.
  const activeRef = useRef(active);
  useEffect(() => {
    activeRef.current = active;
  }, [active]);
  useEffect(() => {
    return () => {
      for (const u of activeRef.current) {
        if (u.tusUpload && (u.status === "uploading" || u.status === "queued")) {
          try {
            u.tusUpload.abort(true);
          } catch {
            // ignore — best-effort cleanup
          }
        }
      }
    };
  }, []);

  function startUpload(file: File, libraryID: string, subpath: string, overwrite: boolean = false) {
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

    // CSRF token. El middleware CSRFProtect del backend rechaza
    // cualquier mutación con cookie auth si NO trae X-CSRF-Token con
    // el valor del cookie hubplay_csrf — defensa contra CSRF
    // cross-origin que un atacante podría dispararar via un <form>
    // o fetch hostil.  tus-js-client no sabe del CSRF, así que se lo
    // pasamos en `headers` y lo añade a TODAS las peticiones
    // (POST creación, PATCH chunks, HEAD, DELETE).
    const csrfToken = readCookie("hubplay_csrf");

    const upload = new tus.Upload(file, {
      endpoint: api.uploadsEndpoint(),
      headers: csrfToken ? { "X-CSRF-Token": csrfToken } : {},
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
        // Overwrite: marca explícita del modal de colisión. Sin esta
        // metadata el pipeline añade sufijo -NNN ante choque; con
        // "true" pisa. tus persiste strings, así que el backend lee
        // overwrite=="true".
        overwrite: overwrite ? "true" : "",
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
  onUpload: (file: File, libraryID: string, subpath: string, overwrite?: boolean) => void;
}

// CollisionDecision: qué hacer con un fichero concreto que ya existe
// en el destino. El modal pre-arranque colecta una decisión por
// fichero (o aplica una a todos con los botones globales).
type CollisionDecision = "overwrite" | "rename" | "skip";

// CollisionItem: pareja fichero local + nombre que choca + decisión
// actual. Lo mantenemos como estructura porque el modal puede mostrar
// múltiples colisiones a la vez con cada una en su estado.
interface CollisionItem {
  file: File;
  libraryID: string;
  subpath: string;
  // existingName == file.name si el sanitizer no lo modificó. Lo
  // duplicamos en la struct para que el modal pinte el nombre tal
  // cual se compara contra el server.
  existingName: string;
  decision: CollisionDecision;
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

  // Modal de colisión: cuando el operador suelta ficheros que ya
  // existen en el destino, pausa los uploads y pregunta una decisión
  // por fichero antes de continuar.
  const [collisions, setCollisions] = useState<CollisionItem[] | null>(null);

  // detectCollisions consulta los ficheros existentes del subpath
  // destino y devuelve los nombres que ya están. Lo invocamos en
  // mismo momento que el upload se va a iniciar — TanStack Query
  // cachea la respuesta del browse, así que typically no genera
  // red extra. Si el query NO está cacheado (caso edge), hacemos
  // un fetch via api.browseUploadFolders directo.
  async function detectCollisions(
    libID: string,
    sub: string,
    candidates: File[],
  ): Promise<File[]> {
    if (!libID) return [];
    try {
      const data = await api.browseUploadFolders(libID, sub);
      const existing = new Set((data.files ?? []).map((f) => f.name));
      return candidates.filter((f) => existing.has(f.name));
    } catch {
      // Si el browse falla, asumimos "no hay colisión conocida" y
      // dejamos que el backend resuelva (con sufijo -NNN). Mejor un
      // upload que aterriza con sufijo que bloquear la operación
      // entera por un fallo de red transitorio del browse.
      return [];
    }
  }

  // Mantén el library seleccionado coherente con la lista (puede
  // cambiar si el admin crea/borra librerías mientras la página está
  // abierta).  Patrón "derive state with previous tracking" — la regla
  // react-hooks/set-state-in-effect prohíbe useEffect + setState.
  const libsKey = libraries.map((l) => l.id).join(",");
  const [prevLibsKey, setPrevLibsKey] = useState(libsKey);
  if (prevLibsKey !== libsKey) {
    setPrevLibsKey(libsKey);
    if (!libraryID || !libraries.find((l) => l.id === libraryID)) {
      setLibraryID(libraries[0]?.id ?? "");
    }
  }

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
      if (!ACCEPTED_EXTENSIONS_SET.has(ext)) {
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

  // launchOrCollide es el camino común a TODOS los uploads (drop
  // dropzone, drop-on-folder, click "Subir N").  Detecta colisiones
  // con los ficheros existentes en el destino y, si las hay, abre
  // el modal. Sin colisiones inicia directo. Los ficheros sin
  // colisión se lanzan inmediatamente; los conflictivos esperan
  // la decisión del modal.
  async function launchOrCollide(filesToUpload: File[], libID: string, sub: string) {
    if (filesToUpload.length === 0 || !libID) return;
    const colliding = await detectCollisions(libID, sub, filesToUpload);
    const collidingSet = new Set(colliding.map((f) => f.name));

    // Lanza inmediatamente los que NO chocan.
    for (const f of filesToUpload) {
      if (!collidingSet.has(f.name)) {
        onUpload(f, libID, sub, false);
      }
    }

    // Para los que chocan, abre el modal de decisión por fichero.
    if (colliding.length > 0) {
      setCollisions(
        colliding.map((f) => ({
          file: f,
          libraryID: libID,
          subpath: sub,
          existingName: f.name,
          decision: "rename",
        })),
      );
    }
  }

  // resolveCollisions aplica las decisiones del modal y lanza los
  // uploads correspondientes. Llamada al confirmar el modal.
  function resolveCollisions(items: CollisionItem[]) {
    for (const it of items) {
      if (it.decision === "skip") continue;
      onUpload(it.file, it.libraryID, it.subpath, it.decision === "overwrite");
    }
    setCollisions(null);
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
    launchOrCollide(valid, libraryID, targetSubpath);
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
    launchOrCollide(files, libraryID, subpath);
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
              // name+size+lastModified hace la key única por fichero
              // sin depender del índice (varios ficheros con mismo
              // nombre pero distinto tamaño no colisionan). El índice
              // sigue haciendo falta para removeFromQueue().
              <li
                key={`${f.name}-${f.size}-${f.lastModified}`}
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

      {collisions && (
        <CollisionModal
          items={collisions}
          onCancel={() => setCollisions(null)}
          onConfirm={resolveCollisions}
        />
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

// ─── CollisionModal ──────────────────────────────────────────────────
//
// Cuando el operador suelta ficheros que ya existen en el destino, este
// modal pide una decisión por fichero antes de continuar. Tres acciones
// por fila:
//   - Sobrescribir: el upload pisa el fichero existente.
//   - Renombrar: el backend añade sufijo "-1", "-2"... (comportamiento
//     por defecto pre-modal, mantenido como red de seguridad).
//   - Saltar: el upload no se inicia para ese fichero.
//
// Tres botones globales arriba: "Sobrescribir todos", "Renombrar
// todos", "Saltar todos" — para subidas masivas con muchas colisiones
// (re-importar una librería desde otro servidor) hacer click por
// fichero sería tedioso.

function CollisionModal({
  items,
  onCancel,
  onConfirm,
}: {
  items: CollisionItem[];
  onCancel: () => void;
  onConfirm: (items: CollisionItem[]) => void;
}) {
  const { t } = useTranslation();
  // `items` se usa SÓLO como semilla inicial — el modal acumula las
  // decisiones del usuario en `state` hasta que pulse Confirmar.
  // Derivarlo en render reiniciaría las decisiones en cada re-render
  // del padre, perdiendo el trabajo del usuario.
  const [state, setState] = useState(items);

  function applyAll(d: CollisionDecision) {
    setState((prev) => prev.map((it) => ({ ...it, decision: d })));
  }

  function setDecision(idx: number, d: CollisionDecision) {
    setState((prev) => {
      const next = [...prev];
      next[idx] = { ...next[idx], decision: d };
      return next;
    });
  }

  const skipCount = state.filter((s) => s.decision === "skip").length;
  const overwriteCount = state.filter((s) => s.decision === "overwrite").length;

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onCancel}
      onKeyDown={(e) => {
        if (e.key === "Escape") onCancel();
      }}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <div
        role="presentation"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        className="w-full max-w-2xl max-h-[80vh] overflow-hidden rounded-lg border border-border bg-bg-base shadow-2xl flex flex-col"
      >
        <header className="border-b border-border px-5 py-4">
          <h3 className="text-base font-semibold text-text-primary">
            {t("uploads.collision.title", {
              count: state.length,
              defaultValue: "{{count}} fichero(s) ya existen en el destino",
            })}
          </h3>
          <p className="mt-1 text-sm text-text-muted">
            {t("uploads.collision.hint", {
              defaultValue:
                "Decide qué hacer con cada uno. Sobrescribir reemplaza el fichero; renombrar añade un sufijo; saltar cancela esa subida.",
            })}
          </p>
        </header>

        <div className="flex flex-wrap items-center gap-2 border-b border-border bg-bg-elevated px-5 py-2 text-xs">
          <span className="text-text-muted mr-2">
            {t("uploads.collision.applyAll", {
              defaultValue: "Aplicar a todos:",
            })}
          </span>
          <button
            type="button"
            onClick={() => applyAll("overwrite")}
            className="rounded-md border border-red-700/50 bg-red-900/20 px-2 py-1 text-red-200 hover:bg-red-900/40"
          >
            {t("uploads.collision.overwrite", { defaultValue: "Sobrescribir" })}
          </button>
          <button
            type="button"
            onClick={() => applyAll("rename")}
            className="rounded-md border border-border bg-bg-base px-2 py-1 text-text-secondary hover:bg-bg-hover"
          >
            {t("uploads.collision.rename", { defaultValue: "Renombrar" })}
          </button>
          <button
            type="button"
            onClick={() => applyAll("skip")}
            className="rounded-md border border-border bg-bg-base px-2 py-1 text-text-muted hover:bg-bg-hover"
          >
            {t("uploads.collision.skip", { defaultValue: "Saltar" })}
          </button>
        </div>

        <ul className="flex-1 overflow-y-auto divide-y divide-border/40">
          {state.map((it, i) => (
            // libraryID + subpath + nombre = ruta destino única, no
            // hay dos colisiones distintas que apunten al mismo path.
            // El índice sigue haciendo falta para setDecision(i, …).
            <li
              key={`${it.libraryID}:${it.subpath}/${it.existingName}`}
              className="flex flex-col gap-2 px-5 py-3 sm:flex-row sm:items-center sm:gap-3"
            >
              <span className="flex flex-1 items-center gap-2 truncate text-sm">
                <FileVideo
                  size={14}
                  className="shrink-0 text-text-muted"
                  aria-hidden
                />
                <span className="truncate font-mono">{it.existingName}</span>
                {it.subpath && (
                  <span className="text-xs text-text-muted shrink-0">
                    /{it.subpath}
                  </span>
                )}
              </span>
              <div className="flex gap-1 shrink-0">
                <CollisionDecisionPill
                  active={it.decision === "overwrite"}
                  tone="danger"
                  label={t("uploads.collision.overwrite", {
                    defaultValue: "Sobrescribir",
                  })}
                  onClick={() => setDecision(i, "overwrite")}
                />
                <CollisionDecisionPill
                  active={it.decision === "rename"}
                  tone="default"
                  label={t("uploads.collision.rename", {
                    defaultValue: "Renombrar",
                  })}
                  onClick={() => setDecision(i, "rename")}
                />
                <CollisionDecisionPill
                  active={it.decision === "skip"}
                  tone="muted"
                  label={t("uploads.collision.skip", {
                    defaultValue: "Saltar",
                  })}
                  onClick={() => setDecision(i, "skip")}
                />
              </div>
            </li>
          ))}
        </ul>

        <footer className="flex items-center justify-between gap-3 border-t border-border bg-bg-elevated px-5 py-3">
          <p className="text-xs text-text-muted">
            {t("uploads.collision.summary", {
              count: state.length,
              skip: skipCount,
              overwrite: overwriteCount,
              defaultValue:
                "{{count}} total · {{overwrite}} sobrescribir · {{skip}} saltar",
            })}
          </p>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={onCancel}>
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
            <Button onClick={() => onConfirm(state)}>
              {t("uploads.collision.confirm", { defaultValue: "Aplicar" })}
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}

function CollisionDecisionPill({
  active,
  tone,
  label,
  onClick,
}: {
  active: boolean;
  tone: "default" | "danger" | "muted";
  label: string;
  onClick: () => void;
}) {
  const tones: Record<typeof tone, string> = {
    default: active
      ? "border-accent bg-accent/15 text-accent"
      : "border-border bg-bg-base text-text-secondary hover:bg-bg-hover",
    danger: active
      ? "border-red-500 bg-red-900/40 text-red-200"
      : "border-border bg-bg-base text-text-secondary hover:bg-red-900/20",
    muted: active
      ? "border-text-muted bg-bg-base text-text-primary"
      : "border-border bg-bg-base text-text-muted hover:bg-bg-hover",
  };
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        "rounded-md border px-2 py-1 text-xs font-medium transition-all",
        tones[tone],
      ].join(" ")}
    >
      {label}
    </button>
  );
}

// ─── helpers ─────────────────────────────────────────────────────────

// readCookie lee un cookie por nombre del document.cookie. tus-js-client
// necesita el valor del CSRF para pasarlo en X-CSRF-Token; en el resto
// del cliente la función equivalente vive en client.ts (no exportada).
// Aquí no la importamos para no acoplar Uploads.tsx con el módulo del
// API client por algo tan pequeño.
function readCookie(name: string): string {
  const m = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
  return m ? decodeURIComponent(m[1]) : "";
}

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
