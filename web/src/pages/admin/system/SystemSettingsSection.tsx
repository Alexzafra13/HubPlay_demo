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

  return (
    <div className="flex flex-col gap-3">
      {data.settings.map((s) => (
        <SettingRow key={s.key} setting={s} />
      ))}
    </div>
  );
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

  const onSave = () => {
    if (!canSave) return;
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
    <div className="flex flex-col gap-4 rounded-[--radius-lg] bg-bg-card border border-border p-5 sm:flex-row sm:items-start sm:gap-6">
      {/* Left column — label + hint + (optional) override badge.
          Stacks above the control on narrow viewports, sits beside
          it on sm+. */}
      <div className="flex flex-col gap-1 sm:flex-1 sm:min-w-0">
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
          <span className="text-xs text-text-muted leading-relaxed">
            {hintText}
          </span>
        )}
      </div>

      {/* Right column — control + actions + footer hint. Fixed width
          on sm+ so every row's control aligns at the same x position
          (Vercel / macOS Settings convention). */}
      <div className="flex flex-col gap-2 sm:w-[280px]">
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
