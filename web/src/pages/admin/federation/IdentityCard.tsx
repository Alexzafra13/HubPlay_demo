import { useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  Check,
  Fingerprint,
  ImagePlus,
  Loader2,
  Pencil,
  Trash2,
  Volume2,
  X,
} from "lucide-react";
import type { FederationServerInfo } from "@/api/types";
import {
  useDeleteServerAvatar,
  useUpdateServerIdentity,
  useUploadServerAvatar,
} from "@/api/hooks/federation";
import { Button, Input, UserAvatar } from "@/components/common";
import { AVATAR_PALETTE } from "@/utils/avatarColor";
import { CopyButton, Label } from "./_shared";

// Mismos límites que el backend (internal/federation/identity_avatar.go)
// y que el avatar de usuario. El navegador rechaza pronto y el admin
// no tiene que esperar al round-trip.
const AVATAR_ACCEPT_MIME = "image/jpeg,image/png,image/webp";
const AVATAR_MAX_BYTES = 5 * 1024 * 1024;

// IdentityCard renders this server's federation identity. La huella
// es el ancla de confianza del protocolo, así que sigue siendo el
// foco visual; encima ahora tiene una cabecera editable con avatar
// (color/iniciales), nombre visible y selector de color — los dos
// últimos persisten via PUT /admin/peers/identity y son lo que ven
// los peers cuando hacen probe.

interface Props {
  info: FederationServerInfo;
}

export function IdentityCard({ info }: Props) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);

  return (
    <div className="rounded-lg border border-border bg-bg-elevated p-5">
      {/* Cabecera: avatar grande + nombre + UUID (compacto). En modo
          editar todo este bloque se reemplaza por el editor inline. */}
      {editing ? (
        <IdentityEditor info={info} onClose={() => setEditing(false)} />
      ) : (
        <IdentityHeader info={info} onEdit={() => setEditing(true)} />
      )}

      {/* Hero: fingerprint. Large mono, accent ring, copy button
          inline. El centro visual de la tarjeta. */}
      <div className="pt-5">
        <div className="flex items-center justify-between gap-2">
          <Label>
            <span className="inline-flex items-center gap-1.5">
              <Fingerprint className="h-3 w-3" />
              {t("admin.federation.identity.fingerprint")}
            </span>
          </Label>
          <CopyButton text={info.pubkey_fingerprint} />
        </div>
        <div className="mt-2 rounded-md border border-accent/30 bg-bg-base px-4 py-3">
          <code className="block break-all text-center font-mono text-xl tracking-[0.15em] text-accent">
            {info.pubkey_fingerprint}
          </code>
        </div>
        <p className="mt-2 text-xs leading-relaxed text-text-muted">
          {t("admin.federation.identity.fingerprintHint")}
        </p>
      </div>

      {/* Phonetic words — chips legibles a brazo. */}
      <div className="mt-5 border-t border-border-subtle pt-5">
        <Label>
          <span className="inline-flex items-center gap-1.5">
            <Volume2 className="h-3 w-3" />
            {t("admin.federation.identity.words")}
          </span>
        </Label>
        <div className="mt-2 flex flex-wrap gap-2">
          {info.pubkey_words.map((word) => (
            <span
              key={word}
              className="rounded-md border border-border bg-bg-base px-3 py-1.5 font-mono text-sm font-semibold tracking-wide text-text-primary"
            >
              {word}
            </span>
          ))}
        </div>
        <p className="mt-2 text-xs leading-relaxed text-text-muted">
          {t("admin.federation.identity.wordsHint")}
        </p>
      </div>
    </div>
  );
}

// IdentityHeader es la vista por defecto: avatar 64px (color +
// iniciales del nombre), nombre del servidor, UUID truncado, y un
// botón "Editar" pequeño que abre el editor inline.
function IdentityHeader({
  info,
  onEdit,
}: {
  info: FederationServerInfo;
  onEdit: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-start gap-4 border-b border-border-subtle pb-4">
      <UserAvatar
        user={{
          username: info.name,
          display_name: info.name,
          avatar_color: info.avatar_color ?? undefined,
          avatar_image_url: info.avatar_image_url ?? null,
        }}
        size="xl"
      />
      <div className="min-w-0 flex-1">
        <Label>{t("admin.federation.identity.name")}</Label>
        <p className="mt-1 truncate text-lg font-semibold text-text-primary">
          {info.name}
        </p>
        <p className="mt-1 truncate font-mono text-[11px] text-text-muted">
          {info.server_uuid}
        </p>
      </div>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        onClick={onEdit}
        className="flex-shrink-0"
      >
        <Pencil className="mr-1.5 h-3.5 w-3.5" aria-hidden />
        {t("common.edit", { defaultValue: "Editar" })}
      </Button>
    </div>
  );
}

// IdentityEditor edita nombre + color del avatar en línea. Vista
// previa en vivo (el avatar reacciona mientras escribes / cambias
// color). Submit hace PUT y actualiza la cache de useServerIdentity
// sin refrescar la pagina.
function IdentityEditor({
  info,
  onClose,
}: {
  info: FederationServerInfo;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const update = useUpdateServerIdentity();
  const uploadAvatar = useUploadServerAvatar();
  const deleteAvatar = useDeleteServerAvatar();
  const [name, setName] = useState(info.name);
  const [color, setColor] = useState(info.avatar_color ?? "");
  const [error, setError] = useState<string | null>(null);
  // Preview en vivo del fichero recién elegido. Se crea con
  // FileReader como data: URL para no depender del backend (todavía
  // no subido). null = sin selección activa.
  const [pendingPreview, setPendingPreview] = useState<string | null>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  // Reset del formulario cuando el prop "info" del backend cambia
  // (e.g. tras un PATCH exitoso del peer remoto). Patrón "derive
  // state with previous tracking" para cumplir con
  // react-hooks/set-state-in-effect — el useEffect+setState
  // equivalente está prohibido por la regla.
  const infoKey = `${info.name}|${info.avatar_color ?? ""}|${info.avatar_image_url ?? ""}`;
  const [prevInfoKey, setPrevInfoKey] = useState(infoKey);
  if (prevInfoKey !== infoKey) {
    setPrevInfoKey(infoKey);
    setName(info.name);
    setColor(info.avatar_color ?? "");
    setError(null);
    setPendingPreview(null);
    setPendingFile(null);
    setAvatarError(null);
  }

  const previewUser = {
    username: info.name,
    display_name: name || info.name,
    avatar_color: color,
    avatar_image_url: info.avatar_image_url ?? null,
  };

  const dirty = name.trim() !== info.name || color !== (info.avatar_color ?? "");
  const saving = update.isPending;
  const uploading = uploadAvatar.isPending;
  const deleting = deleteAvatar.isPending;
  const hasUploadedAvatar = !!info.avatar_image_url;

  function onFilePicked(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    // Reset siempre, así re-elegir el mismo fichero vuelve a
    // disparar onChange (los <input type=file> no lo hacen por
    // defecto si seleccionas el mismo).
    e.target.value = "";
    if (!file) return;

    if (!AVATAR_ACCEPT_MIME.split(",").includes(file.type)) {
      setAvatarError(
        t("admin.federation.identity.avatarUnsupportedType", {
          defaultValue: "Formato no admitido — usa JPEG, PNG o WebP.",
        }),
      );
      return;
    }
    if (file.size > AVATAR_MAX_BYTES) {
      setAvatarError(
        t("admin.federation.identity.avatarTooLarge", {
          defaultValue: "Imagen demasiado grande (máx 5 MB).",
        }),
      );
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      setPendingFile(file);
      setPendingPreview(typeof reader.result === "string" ? reader.result : null);
      setAvatarError(null);
    };
    reader.onerror = () =>
      setAvatarError(
        t("admin.federation.identity.avatarReadError", {
          defaultValue: "No se pudo leer el fichero.",
        }),
      );
    reader.readAsDataURL(file);
  }

  function handleUpload() {
    if (!pendingFile) return;
    setAvatarError(null);
    uploadAvatar.mutate(pendingFile, {
      onSuccess: () => {
        setPendingFile(null);
        setPendingPreview(null);
      },
      onError: (err) => setAvatarError(err.message),
    });
  }

  function handleCancelPending() {
    setPendingFile(null);
    setPendingPreview(null);
    setAvatarError(null);
  }

  function handleRemoveAvatar() {
    setAvatarError(null);
    deleteAvatar.mutate(undefined, {
      onError: (err) => setAvatarError(err.message),
    });
  }

  function handleSave() {
    const trimmed = name.trim();
    if (!trimmed) {
      setError(
        t("admin.federation.identity.nameRequired", {
          defaultValue: "El nombre no puede estar vacío.",
        }),
      );
      return;
    }
    setError(null);
    update.mutate(
      { name: trimmed, avatarColor: color },
      {
        onSuccess: () => onClose(),
        onError: (err) => setError(err.message),
      },
    );
  }

  return (
    <div className="border-b border-border-subtle pb-5">
      <div className="flex items-start gap-4">
        {/* src= sólo cuando hay un fichero pendiente; si no,
            UserAvatar usa lo persistido o iniciales. */}
        <UserAvatar
          user={previewUser}
          size="xl"
          src={pendingPreview ?? undefined}
        />
        <div className="flex-1 space-y-3">
          <Input
            label={t("admin.federation.identity.nameLabel", {
              defaultValue: "Nombre visible para peers",
            })}
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={80}
            placeholder={info.name}
          />
          {/* Foto del servidor: mismo flujo que AccountPanel pero
              compacto e inline con el editor del nombre/color. */}
          <div>
            <Label>
              {t("admin.federation.identity.photoLabel", {
                defaultValue: "Foto del servidor",
              })}
            </Label>
            <p className="mt-1 text-[11px] leading-relaxed text-text-muted">
              {t("admin.federation.identity.photoHint", {
                defaultValue:
                  "Visible para peers cuando hagan probe. JPEG, PNG o WebP, máx 5 MB.",
              })}
            </p>
            <input
              ref={fileInputRef}
              type="file"
              accept={AVATAR_ACCEPT_MIME}
              onChange={onFilePicked}
              className="hidden"
            />
            <div className="mt-2 flex flex-wrap gap-2">
              {!pendingFile && (
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={uploading || deleting}
                >
                  <ImagePlus className="mr-1.5 h-3.5 w-3.5" aria-hidden />
                  {hasUploadedAvatar
                    ? t("admin.federation.identity.changePhoto", {
                        defaultValue: "Cambiar foto",
                      })
                    : t("admin.federation.identity.uploadPhoto", {
                        defaultValue: "Subir foto",
                      })}
                </Button>
              )}
              {pendingFile && (
                <>
                  <Button
                    type="button"
                    size="sm"
                    onClick={handleUpload}
                    isLoading={uploading}
                    disabled={uploading}
                  >
                    {t("admin.federation.identity.confirmUpload", {
                      defaultValue: "Subir esta foto",
                    })}
                  </Button>
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={handleCancelPending}
                    disabled={uploading}
                  >
                    {t("common.cancel", { defaultValue: "Cancelar" })}
                  </Button>
                </>
              )}
              {hasUploadedAvatar && !pendingFile && (
                <Button
                  type="button"
                  variant="danger"
                  size="sm"
                  onClick={handleRemoveAvatar}
                  disabled={uploading || deleting}
                >
                  {deleting ? (
                    <Loader2
                      className="mr-1.5 h-3.5 w-3.5 animate-spin"
                      aria-hidden
                    />
                  ) : (
                    <Trash2 className="mr-1.5 h-3.5 w-3.5" aria-hidden />
                  )}
                  {t("admin.federation.identity.removePhoto", {
                    defaultValue: "Quitar foto",
                  })}
                </Button>
              )}
            </div>
            {avatarError && (
              <p className="mt-2 text-[11px] text-error">{avatarError}</p>
            )}
          </div>
          <div>
            <div className="flex items-baseline justify-between gap-3">
              <Label>
                {t("admin.federation.identity.colorLabel", {
                  defaultValue: "Color del avatar del servidor",
                })}
              </Label>
              {color !== "" && (
                <button
                  type="button"
                  onClick={() => setColor("")}
                  className="text-[11px] text-text-muted underline-offset-2 transition-colors hover:text-text-primary hover:underline"
                >
                  {t("admin.federation.identity.clearColor", {
                    defaultValue: "Quitar (automatico)",
                  })}
                </button>
              )}
            </div>
            <div className="mt-2 flex flex-wrap gap-2.5">
              {AVATAR_PALETTE.map((p) => {
                const selected =
                  color.toLowerCase() === p.background.toLowerCase();
                return (
                  <button
                    type="button"
                    key={p.background}
                    onClick={() => setColor(p.background)}
                    className={[
                      "relative flex h-9 w-9 items-center justify-center rounded-full transition-all",
                      selected
                        ? "scale-110 ring-2 ring-white ring-offset-2 ring-offset-bg-elevated"
                        : "ring-1 ring-white/10 hover:scale-105 hover:ring-white/30",
                    ].join(" ")}
                    style={{ background: p.background }}
                    title={p.label}
                    aria-label={p.label}
                    aria-pressed={selected}
                  >
                    {selected && (
                      <Check
                        className="h-4 w-4 text-white"
                        strokeWidth={3}
                        aria-hidden
                      />
                    )}
                  </button>
                );
              })}
            </div>
            {hasUploadedAvatar && (
              <p className="mt-2 text-[11px] text-text-muted">
                {t("admin.federation.identity.colorBehindPhoto", {
                  defaultValue:
                    "Con foto cargada el color queda detrás como reserva si la imagen no se puede mostrar.",
                })}
              </p>
            )}
          </div>
        </div>
      </div>
      {error && <p className="mt-3 text-xs text-error">{error}</p>}
      <div className="mt-4 flex justify-end gap-2">
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={onClose}
          disabled={saving}
        >
          <X className="mr-1 h-3.5 w-3.5" aria-hidden />
          {t("common.cancel", { defaultValue: "Cancelar" })}
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={handleSave}
          disabled={!dirty || saving}
          isLoading={saving}
        >
          {t("common.save", { defaultValue: "Guardar" })}
        </Button>
      </div>
    </div>
  );
}
