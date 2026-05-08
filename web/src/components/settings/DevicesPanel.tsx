import { useTranslation } from "react-i18next";
import { Laptop, LogOut, Smartphone } from "lucide-react";
import { useMySessions, useRevokeMySession } from "@/api/hooks";
import type { MySession } from "@/api/types";
import { Button, Spinner } from "@/components/common";

// DevicesPanel — the Settings → "Tus dispositivos" panel. Lists
// every active auth session under the calling user (one row per
// refresh token alive in the DB) so the operator can see "where am
// I logged in" and revoke a stale device after losing a phone or
// loaning a laptop. The current session — the one whose refresh
// cookie matches this browser — is marked with an "Este dispositivo"
// pill and gets a confirm-prompt before revoke so a thoughtless
// click doesn't kick the user out of the page they're on.
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
      {rows.map((s) => (
        <DeviceRow key={s.id} session={s} onRevoke={() => handleRevoke(s)} />
      ))}
    </ul>
  );
}

function DeviceRow({
  session,
  onRevoke,
}: {
  session: MySession;
  onRevoke: () => void;
}) {
  const { t } = useTranslation();
  const isMobileLike = looksMobile(session.device_name);
  const Icon = isMobileLike ? Smartphone : Laptop;

  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
      <div className="rounded-md bg-bg-elevated p-2 text-text-secondary">
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-text-primary truncate">
            {session.device_name || t("settings.devices.unknownDevice", {
              defaultValue: "Dispositivo desconocido",
            })}
          </span>
          {session.current && (
            <span className="rounded-full bg-accent/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent">
              {t("settings.devices.thisDevice", {
                defaultValue: "Este dispositivo",
              })}
            </span>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-text-muted">
          {session.ip_address && <span>{session.ip_address}</span>}
          {session.ip_address && <span aria-hidden>·</span>}
          <span>
            {t("settings.devices.lastActive", {
              defaultValue: "Última actividad: {{when}}",
              when: relativeTime(session.last_active_at),
            })}
          </span>
        </div>
      </div>
      <Button
        variant="ghost"
        size="sm"
        onClick={onRevoke}
        title={t("settings.devices.revokeHint", {
          defaultValue: "Cerrar esta sesión",
        })}
      >
        <LogOut className="-ml-0.5 mr-1 h-3.5 w-3.5" />
        {t("settings.devices.revoke", { defaultValue: "Cerrar sesión" })}
      </Button>
    </li>
  );
}

// looksMobile — best-effort heuristic over the User-Agent we stored
// at session creation. Wrong guesses just pick the wrong icon, so
// we don't bother being clever (no UA parser dependency).
function looksMobile(deviceName: string): boolean {
  const lower = deviceName.toLowerCase();
  return /iphone|android|mobi|ipad|tablet/.test(lower);
}

// relativeTime — short "hace 2 min / hace 3 h / hace 5 d" label.
// Same vocabulary as the federation peers list so the user sees a
// consistent register across the app.
function relativeTime(iso: string): string {
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return "—";
  const ageMin = Math.floor((Date.now() - ts) / 60_000);
  if (ageMin < 1) return "ahora";
  if (ageMin < 60) return `hace ${ageMin} min`;
  if (ageMin < 60 * 24) return `hace ${Math.floor(ageMin / 60)} h`;
  return `hace ${Math.floor(ageMin / (60 * 24))} d`;
}
