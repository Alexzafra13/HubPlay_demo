import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  useResetSystemSetting,
  useSystemSettings,
  useUpdateSystemSetting,
} from "@/api/hooks";
import type { SystemSetting } from "@/api/types";
import { Badge, Button, Input, Spinner } from "@/components/common";

// SystemSettingsSection renders the runtime-editable subset of the
// server config (server.base_url, hardware_acceleration.*) the admin
// can change without touching hubplay.yaml. Each row is its own form
// with a Save button so the operator can edit one thing at a time
// and see the result in the same place — same UX shape Plex /
// Jellyfin / Sonarr use.
//
// Layout: a single vertical stack instead of a 2-col card grid.
// Modelled after macOS System Settings and the Vercel project
// settings — each row is a horizontal card with the label/hint on
// the left and the control on the right. Reads at a glance, scales
// to N settings without orphaning a half-empty row, and lets one
// row's hint copy span more characters when it needs to.
//
// Why per-row save (instead of a single global Save at the bottom):
// settings have heterogeneous restart semantics (HWAccel needs a
// container restart, base_url applies live), so a single Save would
// have to either lie about that or render a per-row banner anyway.
export function SystemSettingsSection() {
  const { t } = useTranslation();
  const { data, isLoading, error } = useSystemSettings();

  if (isLoading) {
    return (
      <div className="flex justify-center py-6">
        <Spinner size="md" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <p className="text-sm text-text-muted">
        {error?.message ?? t("admin.system.settingsLoadFailed")}
      </p>
    );
  }

  // Agrupacion por categoria - el panel anterior apilaba 6+ cards
  // full-width que con la descripcion + control + save sumaban
  // ~750px de scroll. La gente con muchos setting trabajan en
  // categorias mentales ("queria ajustar streaming"); agrupandolos
  // visualmente vamos directos a esa lectura y los cards pasan a
  // un grid 2-col compacto.
  const grouped = groupSettings(data.settings);

  return (
    <div className="flex flex-col gap-6">
      {grouped.map((group) => (
        <div key={group.id} className="flex flex-col gap-3">
          <h4 className="text-[10px] font-semibold uppercase tracking-[0.1em] text-text-muted">
            {t(`admin.system.settingsGroup.${group.id}`, {
              defaultValue: group.fallbackLabel,
            })}
          </h4>
          <div className="grid gap-3 lg:grid-cols-2">
            {group.settings.map((s) => (
              <SettingRow key={s.key} setting={s} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// groupSettings asigna cada setting a una categoria mental segun el
// prefijo del key del backend. Settings desconocidos van al grupo
// "general" - mejor mostrarlos que perderlos si añadimos un setting
// nuevo en backend y este grouper no se actualiza.
function groupSettings(settings: SystemSetting[]): SettingGroup[] {
  const buckets: Record<string, SystemSetting[]> = {
    connection: [],
    streaming: [],
    playback: [],
    other: [],
  };
  for (const s of settings) {
    if (s.key.startsWith("server.")) buckets.connection.push(s);
    else if (
      s.key.startsWith("hardware_acceleration.") ||
      s.key.startsWith("streaming.")
    )
      buckets.streaming.push(s);
    else if (s.key.startsWith("playback.")) buckets.playback.push(s);
    else buckets.other.push(s);
  }
  const groups: SettingGroup[] = [];
  if (buckets.connection.length > 0)
    groups.push({
      id: "connection",
      fallbackLabel: "Conexión",
      settings: buckets.connection,
    });
  if (buckets.streaming.length > 0)
    groups.push({
      id: "streaming",
      fallbackLabel: "Streaming",
      settings: buckets.streaming,
    });
  if (buckets.playback.length > 0)
    groups.push({
      id: "playback",
      fallbackLabel: "Reproducción",
      settings: buckets.playback,
    });
  if (buckets.other.length > 0)
    groups.push({
      id: "other",
      fallbackLabel: "Otros",
      settings: buckets.other,
    });
  return groups;
}

interface SettingGroup {
  id: string;
  fallbackLabel: string;
  settings: SystemSetting[];
}

interface SettingRowProps {
  setting: SystemSetting;
}

// SettingRow is the per-key form. Renders an <input> for free-text
// settings and a <select> for enum-shaped ones. When the enum
// collapses to a single allowed value, the select would be a
// pointless one-item dropdown — we show it as plain readonly text
// instead so the row doesn't read as broken.
function SettingRow({ setting }: SettingRowProps) {
  const { t } = useTranslation();
  const update = useUpdateSystemSetting();
  const reset = useResetSystemSetting();

  // Local draft mirrors the effective value but tracks what the user
  // has typed. Resyncing on `setting.effective` (after save / reset
  // mutations rewrite the cache) keeps the input current without a
  // re-mount loop.
  const [draft, setDraft] = useState(setting.effective);
  // Sync local draft when the canonical value flips (admin saved
  // from another tab, or an upstream invalidation rewrote the
  // cache). The lint rule (set-state-in-effect) is too strict for
  // this controlled-mirror pattern; the alternative — keying the
  // whole row by setting.effective — would re-mount the input and
  // wipe the user's in-progress typing on every cache refetch.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setDraft(setting.effective), [setting.effective]);

  const dirty = draft !== setting.effective;
  const isSaving = update.isPending;
  const isResetting = reset.isPending;
  const errorMsg =
    update.error && update.variables?.key === setting.key
      ? update.error.message
      : undefined;

  // Block save on empty drafts. Saving "" would write an empty
  // override, which is semantically distinct from "no override" and
  // almost never what the operator wants — they're trying to clear
  // the value, which is what the Reset button is for. Disabling Save
  // and pointing at Reset is friendlier than letting the write
  // succeed and producing a "configured but blank" effective value.
  const draftEmpty = draft.trim() === "";
  const canSave = dirty && !draftEmpty && !isSaving && !isResetting;

  // Single-allowed-value collapse. When the backend reports just one
  // option (e.g. `hardware_acceleration.preferred` with no GPU
  // detected → only "auto"), a 1-item dropdown reads as a UI bug.
  const isLockedEnum =
    !!setting.allowed_values && setting.allowed_values.length === 1;
  const isMultiEnum =
    !!setting.allowed_values && setting.allowed_values.length > 1;

  // `playback.force_direct_play = true` is the panel's single most
  // dangerous toggle: it bypasses the capability waterfall for every
  // client and means a TV / phone that can't decode an HEVC + EAC3
  // file will just refuse to play. Plenty of operators will flip it
  // thinking "I'll save CPU" and then get bug reports they can't
  // explain. Block the save behind a confirm so the intent is
  // explicit. Disabling it again doesn't need the prompt — going back
  // to the safe default is always OK.
  const requiresDangerousConfirm =
    setting.key === "playback.force_direct_play" && draft === "true";
  const onSave = () => {
    if (!canSave) return;
    if (requiresDangerousConfirm) {
      const ok = window.confirm(
        t("admin.system.forceDirectPlayConfirm", {
          defaultValue:
            "Vas a desactivar la transcodificación para TODOS los clientes. Cualquier reproductor que no pueda decodificar un archivo nativamente dejará de funcionar. ¿Seguro?",
        }),
      );
      if (!ok) return;
    }
    update.mutate({ key: setting.key, value: draft });
  };

  const onReset = () => {
    reset.mutate({ key: setting.key });
  };

  const labelText = t(
    `admin.system.settings.${settingI18nKey(setting.key)}.label`,
    settingFallbackLabel(setting.key),
  );
  const hintText = t(
    `admin.system.settings.${settingI18nKey(setting.key)}.hint`,
    setting.hint,
  );

  return (
    <div className="flex h-full flex-col gap-3 rounded-[--radius-lg] bg-bg-card border border-border p-4">
      {/* Label + badge (top row). Hint queda debajo en texto fino
          - mas legible que en columna izquierda al lado del control,
          y deja el card uniforme en altura entre todos los settings
          del grid 2-col. */}
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-text-primary">
            {labelText}
          </span>
          {setting.override && (
            <Badge variant="success">
              {t("admin.system.overrideBadge", {
                defaultValue: "Personalizado",
              })}
            </Badge>
          )}
        </div>
        {hintText && (
          <span className="text-[11px] text-text-muted leading-relaxed">
            {hintText}
          </span>
        )}
      </div>

      {/* Control + actions + footer. Flex-col en el card vertical
          para que cada card del grid tenga la misma estructura. */}
      <div className="flex flex-col gap-2 mt-auto">
        {/* Force-direct-play danger banner. Renders only when the
            override is currently active so the row visually stands
            out from the rest — the operator should never miss that
            the safety net is off. */}
        {setting.key === "playback.force_direct_play" &&
          setting.effective === "true" && (
            <div
              role="alert"
              className="rounded-md border border-error/40 bg-error/10 px-3 py-2 text-xs text-error"
            >
              {t("admin.system.forceDirectPlayActiveWarning", {
                defaultValue:
                  "Transcodificación deshabilitada para todos los clientes. Si un reproductor no decodifica un archivo, la reproducción fallará sin fallback.",
              })}
            </div>
          )}
        {isMultiEnum ? (
          <select
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            className="rounded-md border border-border bg-bg-base px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent"
            disabled={isSaving || isResetting}
          >
            {setting.allowed_values!.map((v) => (
              <option key={v} value={v}>
                {prettyValue(v, t)}
              </option>
            ))}
          </select>
        ) : isLockedEnum ? (
          <div
            className="rounded-md border border-border bg-bg-base px-3 py-2 text-sm text-text-secondary"
            title={t("admin.system.singleOptionHint", {
              defaultValue:
                "Solo hay una opción disponible — se aplicará automáticamente.",
            })}
          >
            <span className="font-mono">{prettyValue(draft, t)}</span>
            <span className="ml-2 text-xs text-text-muted">
              {t("admin.system.singleOptionLabel", {
                defaultValue: "(única opción detectada)",
              })}
            </span>
          </div>
        ) : (
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={setting.default || t("admin.system.unset")}
            error={errorMsg}
            disabled={isSaving || isResetting}
          />
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            onClick={onSave}
            disabled={!canSave}
            isLoading={isSaving}
            title={
              draftEmpty && dirty
                ? t("admin.system.emptyDraftHint", {
                    defaultValue:
                      "Para borrar el valor usa 'Por defecto'.",
                  })
                : undefined
            }
          >
            {t("admin.system.save")}
          </Button>
          {setting.override && (
            <Button
              size="sm"
              variant="ghost"
              onClick={onReset}
              disabled={isSaving || isResetting}
              isLoading={isResetting}
            >
              {t("admin.system.resetToDefault")}
            </Button>
          )}
        </div>

        {/* Footer hint line: restart pointer wins if both apply
            (operator's first concern is "do I need to restart?"). */}
        {setting.restart_needed && setting.override ? (
          <span className="text-xs text-warning">
            {t("admin.system.restartHint")}
          </span>
        ) : !setting.override && setting.default ? (
          <span className="text-xs text-text-muted">
            {t("admin.system.currentDefault", {
              value: prettyValue(setting.default, t),
            })}
          </span>
        ) : null}
      </div>
    </div>
  );
}

// prettyValue renders the wire-format value with operator-friendly
// labels for well-known tokens. Booleans become Activado /
// Desactivado; everything else passes through. Centralised so the
// dropdown, the locked-enum readout, and the "Por defecto: X"
// footer all read the same.
function prettyValue(
  value: string,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  if (value === "true")
    return t("admin.system.boolEnabled", { defaultValue: "Activado" });
  if (value === "false")
    return t("admin.system.boolDisabled", { defaultValue: "Desactivado" });
  return value;
}

// settingI18nKey maps the dotted backend key to a translation slug —
// `server.base_url` → `serverBaseURL`, etc. Centralised so adding a
// new whitelisted key is just a label/hint pair in the i18n files.
function settingI18nKey(backendKey: string): string {
  switch (backendKey) {
    case "server.base_url":
      return "serverBaseURL";
    case "hardware_acceleration.enabled":
      return "hwAccelEnabled";
    case "hardware_acceleration.preferred":
      return "hwAccelPreferred";
    case "playback.force_direct_play":
      return "forceDirectPlay";
    case "streaming.max_transcode_sessions":
      return "maxTranscodeSessions";
    case "streaming.max_transcode_sessions_per_user":
      return "maxTranscodeSessionsPerUser";
    case "streaming.transcode_preset":
      return "transcodePreset";
    default:
      return backendKey.replace(/[^a-zA-Z0-9]+/g, "_");
  }
}

// settingFallbackLabel ships a sensible default in case the
// translation file is out of sync — better than the raw key showing
// in the UI on a fresh setting addition.
function settingFallbackLabel(backendKey: string): string {
  return backendKey;
}
