import { useTranslation } from "react-i18next";
import { Laptop, LogOut, Smartphone, Tv } from "lucide-react";
import type { MySession } from "@/api/types";
import { Button } from "@/components/common";

// DeviceRow — one row inside any session/device list. Extracted from
// DevicesPanel so the /link page's "Dispositivos vinculados" panel can
// reuse the same visual treatment.
//
// The auth-method badge ("Vínculo dispositivo" vs "Sesión web") is
// the small honest signal that distinguishes a paired TV / CLI from a
// regular browser login — without it the Settings panel just lists
// every refresh token undifferentiated, which feels unprofessional in
// a household with several devices. The badge is hidden on the /link
// page because that whole list is already filtered to device-links.
export function DeviceRow({
  session,
  onRevoke,
  showAuthMethodBadge = true,
}: {
  session: MySession;
  onRevoke: () => void;
  showAuthMethodBadge?: boolean;
}) {
  const { t } = useTranslation();
  const isMobileLike = looksMobile(session.device_name);
  const isDeviceLink = session.auth_method === "device_link";
  const Icon = isDeviceLink ? Tv : isMobileLike ? Smartphone : Laptop;

  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
      <div className="rounded-md bg-bg-elevated p-2 text-text-secondary">
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-text-primary truncate">
            {session.device_name ||
              t("settings.devices.unknownDevice", {
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
          {showAuthMethodBadge && (
            <span
              className={
                isDeviceLink
                  ? "rounded-full bg-blue-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-blue-300"
                  : "rounded-full bg-text-muted/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted"
              }
            >
              {isDeviceLink
                ? t("settings.devices.badge.deviceLink", {
                    defaultValue: "Vínculo dispositivo",
                  })
                : t("settings.devices.badge.web", {
                    defaultValue: "Sesión web",
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
// at session creation. Wrong guesses just pick the wrong icon, so we
// don't bother being clever (no UA parser dependency).
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
