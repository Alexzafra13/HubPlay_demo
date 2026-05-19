import { useTranslation } from "react-i18next";
import { ArrowUpCircle, ExternalLink, RefreshCw, ShieldCheck } from "lucide-react";

import { useUpdateStatus, useCheckUpdatesNow } from "@/api/hooks";
import { Button } from "@/components/common";

/**
 * UpdateBanner — banner discreto que aparece en el panel admin cuando
 * el update checker detecta una versión nueva. Pintamos tres estados:
 *
 *   1. has_update=true               → tarjeta llamativa con CTA.
 *   2. check_enabled=false           → mensaje informativo (dev build /
 *                                      checker deshabilitado).
 *   3. up-to-date (default)          → tarjeta neutra "estás al día"
 *                                      con botón "Comprobar ahora".
 *
 * Rate-limit del backend: el botón "Comprobar ahora" puede devolver
 * error 429 si el operador clicka más rápido que 1/minuto. El mensaje
 * lo mostramos al lado del botón.
 */
export function UpdateBanner() {
  const { t } = useTranslation();
  const { data, isLoading } = useUpdateStatus();
  const check = useCheckUpdatesNow();

  if (isLoading || !data) return null;

  // Checker deshabilitado (dev build, repo no configurado): mensaje
  // sutil informando — no spammeamos al developer.
  if (!data.check_enabled) {
    return (
      <div className="rounded-[--radius-md] border border-border bg-bg-elevated px-4 py-3 text-sm text-text-muted flex items-center gap-2">
        <ShieldCheck size={16} aria-hidden />
        <span>
          {t("admin.updates.disabled", {
            current: data.current,
            defaultValue:
              "Build {{current}} — comprobación de updates deshabilitada.",
          })}
        </span>
      </div>
    );
  }

  // Update disponible — banner llamativo.
  if (data.has_update) {
    return (
      <div
        role="alert"
        className="rounded-[--radius-md] border border-accent/40 bg-accent/10 px-4 py-3 text-sm"
      >
        <div className="flex items-start gap-3">
          <ArrowUpCircle size={20} className="text-accent shrink-0 mt-0.5" aria-hidden />
          <div className="flex-1 min-w-0">
            <p className="font-semibold text-text-primary">
              {t("admin.updates.available", {
                latest: data.latest,
                defaultValue: "Nueva versión disponible: {{latest}}",
              })}
            </p>
            <p className="text-text-muted mt-0.5">
              {t("admin.updates.runningVersion", {
                current: data.current,
                defaultValue: "Estás corriendo {{current}}.",
              })}
            </p>
          </div>
          <div className="flex gap-2 shrink-0">
            {data.release_url && (
              <a
                href={data.release_url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 rounded-md border border-border px-3 py-1.5 text-sm hover:bg-bg-hover"
              >
                <ExternalLink size={14} aria-hidden />
                {t("admin.updates.viewRelease", {
                  defaultValue: "Ver release",
                })}
              </a>
            )}
            <Button
              size="sm"
              variant="primary"
              onClick={() => {
                if (data.release_url) {
                  window.open(data.release_url, "_blank", "noopener,noreferrer");
                }
              }}
            >
              {t("admin.updates.howToUpdate", {
                defaultValue: "Cómo actualizar",
              })}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  // Al día — tarjeta neutra con botón de comprobación manual.
  return (
    <div className="rounded-[--radius-md] border border-border bg-bg-elevated px-4 py-3 text-sm">
      <div className="flex items-center gap-3">
        <ShieldCheck size={18} className="text-green-500 shrink-0" aria-hidden />
        <div className="flex-1 min-w-0">
          <p className="text-text-primary">
            {t("admin.updates.upToDate", {
              current: data.current,
              defaultValue: "Estás al día — corriendo {{current}}",
            })}
          </p>
          {data.last_checked && (
            <p className="text-text-muted text-xs mt-0.5">
              {t("admin.updates.lastChecked", {
                when: new Date(data.last_checked).toLocaleString(),
                defaultValue: "Última comprobación: {{when}}",
              })}
            </p>
          )}
          {check.error && (
            <p className="text-red-500 text-xs mt-1">
              {check.error.message.includes("429")
                ? t("admin.updates.rateLimited", {
                    defaultValue: "Espera un minuto antes de reintentar.",
                  })
                : check.error.message}
            </p>
          )}
        </div>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => check.mutate()}
          disabled={check.isPending}
          aria-label={t("admin.updates.checkNow", {
            defaultValue: "Comprobar ahora",
          })}
        >
          <RefreshCw
            size={14}
            className={check.isPending ? "animate-spin" : ""}
            aria-hidden
          />
        </Button>
      </div>
    </div>
  );
}
