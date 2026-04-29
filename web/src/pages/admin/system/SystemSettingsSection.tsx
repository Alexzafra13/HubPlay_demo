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
// with a Save button so the operator can edit one thing at a time and
// see the result in the same place — same UX shape Plex / Jellyfin /
// Sonarr use for "current effective value + override badge + save".
//
// Why per-row save (instead of a single global Save at the bottom):
// settings have heterogeneous restart semantics (HWAccel needs a
// container restart, base_url applies live), so a single Save would
// have to either lie about that or render a per-row banner anyway.
// Splitting at the form level keeps the affordance honest.
export function SystemSettingsSection() {
  const { t } = useTranslation();
  const { data, isLoading, error } = useSystemSettings();

  if (isLoading) {
    return (
      <section className="flex flex-col gap-3">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
          {t("admin.system.sectionSettings")}
        </h3>
        <div className="flex justify-center py-6">
          <Spinner size="md" />
        </div>
      </section>
    );
  }

  if (error || !data) {
    return (
      <section className="flex flex-col gap-3">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
          {t("admin.system.sectionSettings")}
        </h3>
        <p className="text-sm text-text-muted">
          {error?.message ?? t("admin.system.settingsLoadFailed")}
        </p>
      </section>
    );
  }

  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
        {t("admin.system.sectionSettings")}
      </h3>
      <div className="grid gap-4 lg:grid-cols-2">
        {data.settings.map((s) => (
          <SettingRow key={s.key} setting={s} />
        ))}
      </div>
    </section>
  );
}

interface SettingRowProps {
  setting: SystemSetting;
}

// SettingRow is the per-key form. Renders an <input> for free-text
// settings and a <select> for enum-shaped ones (allowed_values). The
// "dirty" state pins the Save button visible only after the operator
// has actually changed something — clean-slate render shows the value
// as read-only-ish so a stray click on a stale value doesn't trigger
// a no-op write.
function SettingRow({ setting }: SettingRowProps) {
  const { t } = useTranslation();
  const update = useUpdateSystemSetting();
  const reset = useResetSystemSetting();

  // Local draft mirrors the effective value but tracks what the user
  // has typed. Resyncing on `setting.effective` (after save / reset
  // mutations rewrite the cache) keeps the input current without a
  // re-mount loop.
  const [draft, setDraft] = useState(setting.effective);
  useEffect(() => {
    setDraft(setting.effective);
  }, [setting.effective]);

  const dirty = draft !== setting.effective;
  const isSaving = update.isPending;
  const isResetting = reset.isPending;
  const errorMsg =
    update.error && update.variables?.key === setting.key
      ? update.error.message
      : undefined;

  const onSave = () => {
    if (!dirty) return;
    update.mutate({ key: setting.key, value: draft });
  };

  const onReset = () => {
    reset.mutate({ key: setting.key });
  };

  return (
    <div className="flex flex-col gap-3 rounded-[--radius-lg] bg-bg-card border border-border p-5">
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium uppercase tracking-wider text-text-muted">
            {t(`admin.system.settings.${settingI18nKey(setting.key)}.label`, settingFallbackLabel(setting.key))}
          </span>
          <span className="text-xs text-text-muted">
            {t(`admin.system.settings.${settingI18nKey(setting.key)}.hint`, setting.hint)}
          </span>
        </div>
        <div className="flex flex-shrink-0 items-center gap-2">
          {setting.override ? (
            <Badge variant="success">{t("admin.system.overrideBadge")}</Badge>
          ) : (
            <Badge variant="default">{t("admin.system.defaultBadge")}</Badge>
          )}
        </div>
      </div>

      {setting.allowed_values && setting.allowed_values.length > 0 ? (
        <select
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className="rounded-md border border-border bg-bg-base px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          disabled={isSaving || isResetting}
        >
          {setting.allowed_values.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
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
          disabled={!dirty || isSaving || isResetting}
          isLoading={isSaving}
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
        {setting.restart_needed && setting.override && (
          <span className="text-xs text-warning">
            {t("admin.system.restartHint")}
          </span>
        )}
        {!setting.override && setting.default && (
          <span className="text-xs text-text-muted">
            {t("admin.system.currentDefault", { value: setting.default })}
          </span>
        )}
      </div>
    </div>
  );
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
