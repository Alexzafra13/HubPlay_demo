import { useRef, useState } from "react";
import type { ChangeEvent, FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  useDeleteMyAvatar,
  useMe,
  useSetUserAvatarColor,
  useSetUserDisplayName,
  useUploadMyAvatar,
} from "@/api/hooks";
import { Badge, Button, Input, UserAvatar } from "@/components/common";
import { AVATAR_PALETTE } from "@/utils/avatarColor";
import { Check, ImagePlus, Loader2, Trash2 } from "lucide-react";

// Tipos MIME aceptados y tope de tamaño — replica del backend
// (internal/user/service.go) para que el navegador rechace temprano
// y el operador no tenga que esperar el round-trip para enterarse.
const ACCEPT_MIME = "image/jpeg,image/png,image/webp";
const MAX_BYTES = 5 * 1024 * 1024;

// Panel "Mi cuenta" — el usuario edita aquí su nombre visible, el
// color del avatar y (opcionalmente) sube una foto propia. La foto
// tiene prioridad sobre el color; mientras no haya foto cargada, el
// círculo es iniciales sobre el color elegido (o automático).
export function AccountPanel() {
  const { t } = useTranslation();
  const { data: me, isLoading } = useMe();
  const setDisplayName = useSetUserDisplayName();
  const setAvatarColor = useSetUserAvatarColor();
  const uploadAvatar = useUploadMyAvatar();
  const deleteAvatar = useDeleteMyAvatar();

  const [name, setName] = useState("");
  const [color, setColor] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  // Previsualización en vivo del fichero elegido en el picker. Se
  // crea con FileReader como data: URL para no depender del backend
  // (todavía no subido). null = sin selección activa; el avatar
  // muestra entonces lo del server o las iniciales.
  const [pendingPreview, setPendingPreview] = useState<string | null>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Identidad sembrada; mismo patrón que en Fase 1.
  const [seededId, setSeededId] = useState<string | null>(null);

  if (me && me.id !== seededId) {
    setSeededId(me.id);
    setName(me.display_name ?? "");
    setColor(me.avatar_color ?? "");
    setError(null);
    setSaved(false);
    setPendingPreview(null);
    setPendingFile(null);
    setAvatarError(null);
  }

  if (isLoading || !me) {
    return (
      <div className="rounded-[--radius-lg] border border-border bg-bg-card p-6 text-sm text-text-muted">
        {t("common.loading", { defaultValue: "Cargando…" })}
      </div>
    );
  }

  const nameDirty = name.trim() !== (me.display_name ?? "");
  const colorDirty = color !== (me.avatar_color ?? "");
  const dirty = nameDirty || colorDirty;
  const saving = setDisplayName.isPending || setAvatarColor.isPending;
  const hasUploadedAvatar = !!me.avatar_image_url;

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!me) return;
    const next = name.trim();
    if (!next) {
      setError(
        t("settings.accountPanel.nameRequired", {
          defaultValue: "El nombre no puede estar vacío.",
        }),
      );
      return;
    }
    setError(null);
    setSaved(false);
    const tasks: Promise<unknown>[] = [];
    if (nameDirty) {
      tasks.push(
        setDisplayName.mutateAsync({ userId: me.id, displayName: next }),
      );
    }
    if (colorDirty) {
      tasks.push(setAvatarColor.mutateAsync({ userId: me.id, hex: color }));
    }
    Promise.all(tasks)
      .then(() => setSaved(true))
      .catch((err: Error) => setError(err.message));
  }

  function onFilePicked(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    // Reset el input siempre, así el mismo fichero re-elegido vuelve
    // a disparar onChange (los <input type=file> no lo hacen por
    // defecto si seleccionas el mismo).
    e.target.value = "";
    if (!file) return;

    if (!ACCEPT_MIME.split(",").includes(file.type)) {
      setAvatarError(
        t("settings.accountPanel.avatarUnsupportedType", {
          defaultValue: "Formato no admitido — usa JPEG, PNG o WebP.",
        }),
      );
      return;
    }
    if (file.size > MAX_BYTES) {
      setAvatarError(
        t("settings.accountPanel.avatarTooLarge", {
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
        t("settings.accountPanel.avatarReadError", {
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

  // Para el preview del avatar: si hay un fichero recién elegido,
  // usamos su data URL (todavía no subido); si no, dejamos que
  // UserAvatar use lo que venga en `me`.
  const previewUser = {
    username: me.username,
    display_name: name || me.display_name,
    avatar_color: color,
    avatar_image_url: me.avatar_image_url,
  };
  const displayName = name.trim() || me.username;
  const uploading = uploadAvatar.isPending;
  const deleting = deleteAvatar.isPending;

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[--radius-lg] border border-border bg-bg-card p-6 flex flex-col gap-6"
    >
      {/* Cabecera del panel: avatar grande (con preview en vivo si
          hay fichero pendiente) + nombre + rol como badge. */}
      <div className="flex items-center gap-4">
        {/* src= sólo cuando hay un fichero pendiente; si no, pasamos
            `undefined` para que UserAvatar use `user.avatar_image_url`.
            Pasar `null` aquí suprimiría el avatar guardado y dejaría
            sólo las iniciales (no es lo que queremos). */}
        <UserAvatar
          user={previewUser}
          size="xl"
          src={pendingPreview ?? undefined}
        />
        <div className="flex flex-col gap-1 min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-lg font-semibold text-text-primary truncate">
              {displayName}
            </span>
            <Badge variant={me.role === "admin" ? "warning" : "default"}>
              {me.role}
            </Badge>
          </div>
          <span className="text-xs text-text-muted">
            {t("settings.username", { defaultValue: "Usuario" })}: {me.username}
          </span>
        </div>
      </div>

      {/* Subir foto / quitar foto */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-text-secondary">
          {t("settings.accountPanel.photoTitle", {
            defaultValue: "Foto de perfil",
          })}
        </span>
        <p className="text-xs text-text-muted">
          {t("settings.accountPanel.photoHint", {
            defaultValue:
              "Usa una imagen cuadrada para mejor resultado. JPEG, PNG o WebP, máx 5 MB.",
          })}
        </p>
        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPT_MIME}
          onChange={onFilePicked}
          className="hidden"
        />
        <div className="flex flex-wrap gap-2">
          {!pendingFile && (
            <Button
              type="button"
              variant="secondary"
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading || deleting}
            >
              <ImagePlus className="size-4 mr-1.5" aria-hidden />
              {hasUploadedAvatar
                ? t("settings.accountPanel.changePhoto", {
                    defaultValue: "Cambiar foto",
                  })
                : t("settings.accountPanel.uploadPhoto", {
                    defaultValue: "Subir foto",
                  })}
            </Button>
          )}
          {pendingFile && (
            <>
              <Button
                type="button"
                onClick={handleUpload}
                isLoading={uploading}
                disabled={uploading}
              >
                {t("settings.accountPanel.confirmUpload", {
                  defaultValue: "Subir esta foto",
                })}
              </Button>
              <Button
                type="button"
                variant="secondary"
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
              onClick={handleRemoveAvatar}
              disabled={uploading || deleting}
            >
              {deleting ? (
                <Loader2 className="size-4 mr-1.5 animate-spin" aria-hidden />
              ) : (
                <Trash2 className="size-4 mr-1.5" aria-hidden />
              )}
              {t("settings.accountPanel.removePhoto", {
                defaultValue: "Quitar foto",
              })}
            </Button>
          )}
        </div>
        {avatarError && <p className="text-xs text-error">{avatarError}</p>}
      </div>

      <Input
        label={t("settings.displayName", { defaultValue: "Nombre para mostrar" })}
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={64}
        placeholder={me.username}
      />

      <div className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm font-medium text-text-secondary">
            {t("settings.accountPanel.avatarColor", {
              defaultValue: "Color del avatar",
            })}
          </span>
          {color !== "" && (
            <button
              type="button"
              onClick={() => setColor("")}
              className="text-xs text-text-muted hover:text-text-primary underline-offset-2 hover:underline transition-colors"
            >
              {t("settings.accountPanel.clearColor", {
                defaultValue: "Quitar selección (automático)",
              })}
            </button>
          )}
        </div>
        <div className="flex flex-wrap gap-3">
          {AVATAR_PALETTE.map((p) => {
            const selected = color.toLowerCase() === p.background.toLowerCase();
            return (
              <button
                type="button"
                key={p.background}
                onClick={() => setColor(p.background)}
                className={[
                  "relative flex size-10 items-center justify-center rounded-full transition-all",
                  selected
                    ? "ring-2 ring-white ring-offset-2 ring-offset-bg-card scale-110"
                    : "ring-1 ring-white/10 hover:scale-105 hover:ring-white/30",
                ].join(" ")}
                style={{ background: p.background }}
                title={p.label}
                aria-label={p.label}
                aria-pressed={selected}
              >
                {selected && <Check className="size-5 text-white" strokeWidth={3} />}
              </button>
            );
          })}
        </div>
        {color === "" && (
          <p className="text-xs text-text-muted">
            {t("settings.accountPanel.autoHint", {
              defaultValue:
                "Sin color elegido — se usa uno automático único derivado de tu usuario.",
            })}
          </p>
        )}
        {hasUploadedAvatar && (
          <p className="text-xs text-text-muted">
            {t("settings.accountPanel.colorBehindPhoto", {
              defaultValue:
                "Con foto cargada el color queda detrás como reserva si la imagen no se puede mostrar.",
            })}
          </p>
        )}
      </div>

      {error && <p className="text-xs text-error">{error}</p>}
      {saved && !dirty && (
        <p className="text-xs text-success">
          {t("settings.accountPanel.saved", { defaultValue: "Guardado." })}
        </p>
      )}

      <div className="flex justify-end gap-3">
        <Button
          type="button"
          variant="secondary"
          disabled={!dirty || saving}
          onClick={() => {
            setName(me.display_name ?? "");
            setColor(me.avatar_color ?? "");
            setError(null);
            setSaved(false);
          }}
        >
          {t("common.cancel", { defaultValue: "Cancelar" })}
        </Button>
        <Button type="submit" disabled={!dirty || saving} isLoading={saving}>
          {t("common.save", { defaultValue: "Guardar" })}
        </Button>
      </div>
    </form>
  );
}
