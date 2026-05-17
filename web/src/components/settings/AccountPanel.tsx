import { useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  useMe,
  useSetUserAvatarColor,
  useSetUserDisplayName,
} from "@/api/hooks";
import { Button, Input, UserAvatar } from "@/components/common";
import { AVATAR_PALETTE } from "@/utils/avatarColor";

// Panel "Mi cuenta" — el usuario edita aquí su nombre visible y el
// color de su avatar. Antes vivía como modal "Personalizar" en el
// panel admin; lo movemos al perfil del propio usuario porque cada
// uno decide cómo quiere aparecer. En Fase 2 se añade la subida de
// imagen propia justo encima del bloque de colores.
export function AccountPanel() {
  const { t } = useTranslation();
  const { data: me, isLoading } = useMe();
  const setDisplayName = useSetUserDisplayName();
  const setAvatarColor = useSetUserAvatarColor();

  const [name, setName] = useState("");
  const [color, setColor] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  // Identidad del /me que ya sembramos al formulario. Si cambia
  // (login distinto, switch de perfil), reseedeamos en render —
  // patrón oficial de React para "adjusting state when prop
  // changes" sin caer en useEffect → setState cascada.
  const [seededId, setSeededId] = useState<string | null>(null);

  if (me && me.id !== seededId) {
    setSeededId(me.id);
    setName(me.display_name ?? "");
    setColor(me.avatar_color ?? "");
    setError(null);
    setSaved(false);
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

  // Vista en vivo: el avatar de la izquierda usa los valores del
  // formulario (no los del servidor) para que el cambio de color
  // sea inmediato al hacer click en un swatch.
  const previewUser = {
    username: me.username,
    display_name: name || me.display_name,
    avatar_color: color,
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[--radius-lg] border border-border bg-bg-card p-6 flex flex-col gap-6"
    >
      <div className="flex items-center gap-4">
        <UserAvatar user={previewUser} size="xl" />
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="text-sm text-text-muted">
            {t("settings.username", { defaultValue: "Usuario" })}
          </span>
          <span className="font-medium text-text-primary truncate">
            {me.username}
          </span>
        </div>
      </div>

      <Input
        label={t("settings.displayName", { defaultValue: "Nombre para mostrar" })}
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={64}
        placeholder={me.username}
      />

      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-text-secondary">
          {t("settings.accountPanel.avatarColor", {
            defaultValue: "Color del avatar",
          })}
        </span>
        <p className="text-xs text-text-muted">
          {t("settings.accountPanel.avatarColorHint", {
            defaultValue:
              "Elige un color o deja Auto para que se asigne uno único a partir de tu usuario.",
          })}
        </p>
        <div className="grid grid-cols-7 gap-2 sm:grid-cols-8">
          <button
            type="button"
            onClick={() => setColor("")}
            className={[
              "h-9 w-9 rounded-full border-2 text-[10px] font-medium transition-all",
              color === ""
                ? "border-accent ring-2 ring-accent/30 text-text-primary"
                : "border-border-subtle text-text-muted hover:border-border",
            ].join(" ")}
            title={t("settings.accountPanel.avatarColorAutoHint", {
              defaultValue: "Color automático según tu usuario.",
            })}
            aria-label={t("settings.accountPanel.avatarColorAuto", {
              defaultValue: "Auto",
            })}
          >
            A
          </button>
          {AVATAR_PALETTE.map((p) => {
            const selected = color.toLowerCase() === p.background.toLowerCase();
            return (
              <button
                type="button"
                key={p.background}
                onClick={() => setColor(p.background)}
                className={[
                  "h-9 w-9 rounded-full border-2 transition-all",
                  selected
                    ? "border-white scale-110 ring-2 ring-white/30"
                    : "border-transparent hover:scale-105",
                ].join(" ")}
                style={{ background: p.background }}
                title={p.label}
                aria-label={p.label}
                aria-pressed={selected}
              />
            );
          })}
        </div>
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
