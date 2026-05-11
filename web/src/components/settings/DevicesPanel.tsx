import { useTranslation } from "react-i18next";
import { useMySessions, useRevokeMySession } from "@/api/hooks";
import type { MySession } from "@/api/types";
import { Spinner } from "@/components/common";
import { DeviceRow } from "./DeviceRow";

// DevicesPanel — the Settings → "Tus dispositivos" panel. Lists
// every active auth session under the calling user (one row per
// refresh token alive in the DB) so the operator can see "where am
// I logged in" and revoke a stale device after losing a phone or
// loaning a laptop. Each row shows an auth-method badge so paired
// TVs / consoles are visually distinct from regular web logins.
//
// The current session — the one whose refresh cookie matches this
// browser — is marked with an "Este dispositivo" pill and gets a
// confirm-prompt before revoke so a thoughtless click doesn't kick
// the user out of the page they're on.
//
// Distinct from /admin/system → Sesiones activas: that surface
// lists PLAYBACK sessions across the server (admin scope), this one
// lists LOGINS for one user.

export function DevicesPanel() {
  const { t } = useTranslation();
  const { data: sessions, isLoading, error } = useMySessions();
  const revoke = useRevokeMySession();

  if (isLoading) {
    return (
      <div className="flex justify-center py-6">
        <Spinner />
      </div>
    );
  }

  if (error) {
    return (
      <p className="text-sm text-error">
        {t("settings.devices.loadFailed", {
          defaultValue: "No pudimos cargar tus dispositivos.",
        })}
      </p>
    );
  }

  const rows = sessions ?? [];
  if (rows.length === 0) {
    return (
      <p className="text-sm text-text-muted">
        {t("settings.devices.empty", {
          defaultValue: "No hay sesiones activas en este momento.",
        })}
      </p>
    );
  }

  // Group device-linked rows ahead of password ones so a household
  // with several paired devices doesn't bury them under "current
  // browser" duplicates.
  const sorted = [...rows].sort((a, b) => {
    if (a.auth_method !== b.auth_method) {
      return a.auth_method === "device_link" ? -1 : 1;
    }
    return (
      new Date(b.last_active_at).getTime() -
      new Date(a.last_active_at).getTime()
    );
  });

  const handleRevoke = (s: MySession) => {
    if (s.current) {
      const ok = window.confirm(
        t("settings.devices.revokeSelfConfirm", {
          defaultValue:
            "Vas a cerrar la sesión de este mismo dispositivo. Te tocará volver a iniciar sesión. ¿Continuar?",
        }),
      );
      if (!ok) return;
    }
    revoke.mutate({ sessionId: s.id });
  };

  return (
    <ul className="flex flex-col divide-y divide-border-subtle rounded-[--radius-lg] border border-border bg-bg-card">
      {sorted.map((s) => (
        <DeviceRow key={s.id} session={s} onRevoke={() => handleRevoke(s)} />
      ))}
    </ul>
  );
}
