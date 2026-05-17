import { useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  useMe,
  useSetUserAvatarColor,
  useSetUserDisplayName,
} from "@/api/hooks";
import { Badge, Button, Input, UserAvatar } from "@/components/common";
import { AVATAR_PALETTE } from "@/utils/avatarColor";
import { Check } from "lucide-react";

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
  // Identidad del /me ya sembrada al formulario. Si cambia (login
  // distinto, switch de perfil), reseedeamos en render — patrón
  // oficial de React para "adjusting state when prop changes" sin
  // caer en useEffect → setState cascada.
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

  // Preview en vivo: el avatar de la cabecera usa los valores del
  // formulario (no los del servidor) para que el cambio sea visible
  // al hacer click en un swatch.
  const previewUser = {
    username: me.username,
    display_name: name || me.display_name,
    avatar_color: color,
  };
  const displayName = name.trim() || me.username;

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[--radius-lg] border border-border bg-bg-card p-6 flex flex-col gap-6"
    >
      {/* Cabecera del panel: avatar grande + nombre + rol como
          badge. La etiqueta "Usuario" + el username crudo van
          debajo, pequeños, porque son referencia técnica (login)
          mientras que display_name es la identidad visible. */}
      <div className="flex items-center gap-4">
        <UserAvatar user={previewUser} size="xl" />
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
          {/* Botón "limpiar" sólo aparece cuando hay un color
              elegido. Limpiar = volver al color automático derivado
              del usuario; equivale al antiguo botón "A" pero como
              acción explícita en vez de tile siempre presente. */}
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
                  "relative flex h-10 w-10 items-center justify-center rounded-full transition-all",
                  selected
                    ? "ring-2 ring-white ring-offset-2 ring-offset-bg-card scale-110"
                    : "ring-1 ring-white/10 hover:scale-105 hover:ring-white/30",
                ].join(" ")}
                style={{ background: p.background }}
                title={p.label}
                aria-label={p.label}
                aria-pressed={selected}
              >
                {selected && <Check className="h-5 w-5 text-white" strokeWidth={3} />}
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
