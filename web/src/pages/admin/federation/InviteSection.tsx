import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertCircle, Clock, Mail, Plus, Shield } from "lucide-react";
import { useGenerateInvite, useListInvites } from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { CopyButton, ErrorBanner } from "./_shared";
import type { FederationInvite } from "@/api/types";

// InviteSection — el panel donde el admin local genera un codigo de
// un solo uso y se lo pasa por canal seguro al admin del otro
// servidor para emparejar. Renderiza bare (no <section>/h3) porque
// vive dentro de una Radix Tab que ya anuncia "Generar invite".
//
// Layout:
//   - Hint corto sobre que es esto + como usarlo.
//   - Boton primario "Generar nuevo invite".
//   - Lista de codigos activos (los generados que aun no se han
//     consumido ni caducado), con el ultimo destacado en un card y
//     el resto en lista compacta. Cada uno con copy + contador.

export function InviteSection() {
  const { t } = useTranslation();
  const invites = useListInvites();
  const generate = useGenerateInvite();
  const activeInvites = invites.data ?? [];
  // El mas reciente se trata como "el activo": va arriba en un
  // card mas grande para que la accion de copy + share sea
  // obvia tras pulsar "Generar".
  const [latest, ...rest] = sortByExpiry(activeInvites);

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm leading-relaxed text-text-muted">
        {t("admin.federation.invite.description")}
      </p>

      {/* Botón generar. Si ya hay códigos activos, ofrecemos
          "Generar OTRO" para no esconder accidentalmente la
          posibilidad de tener varios pendientes. */}
      <div>
        <Button
          variant="primary"
          onClick={() => generate.mutate()}
          disabled={generate.isPending}
        >
          <Plus className="-ml-1 mr-1.5 size-4" />
          {generate.isPending
            ? t("admin.federation.invite.generating")
            : activeInvites.length > 0
              ? t("admin.federation.invite.generateAnother", {
                  defaultValue: "Generar otro código",
                })
              : t("admin.federation.invite.generate")}
        </Button>
      </div>

      {generate.error && <ErrorBanner message={String(generate.error)} />}

      {/* Card destacado del ultimo invite. Lo mas comun tras pulsar
          generate es copiar + compartir AHORA — la jerarquia visual
          le da al codigo el protagonismo que necesita. */}
      {latest && <LatestInviteCard invite={latest} />}

      {/* Resto de invites activos como lista compacta. Solo se
          ven si el admin generó varios sin compartirlos. */}
      {rest.length > 0 && (
        <div className="flex flex-col gap-2 border-t border-border-subtle pt-4">
          <p className="text-xs font-semibold uppercase tracking-wider text-text-muted">
            {t("admin.federation.invite.otherActiveHeading", {
              defaultValue: "Otros códigos activos",
            })}
          </p>
          <ul className="flex flex-col gap-2">
            {rest.map((inv) => (
              <li
                key={inv.id}
                className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-bg-base px-3 py-2.5"
              >
                <code className="flex-1 break-all font-mono text-sm font-medium text-accent">
                  {inv.code}
                </code>
                <ExpiryChip expiresAt={inv.expires_at} />
                <CopyButton text={inv.code} />
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// LatestInviteCard — destaca el codigo recien generado con un card
// grande: tipografia mono + tamaño ancho + boton copy prominente +
// contador de tiempo restante con severidad. Una linea de "cómo
// usarlo" debajo asegura que el admin sepa que hacer.
function LatestInviteCard({ invite }: { invite: FederationInvite }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-accent/40 bg-accent/5 p-4">
      <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-text-muted">
        <Shield className="size-3 text-accent" />
        {t("admin.federation.invite.latestLabel", {
          defaultValue: "Código activo",
        })}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-3">
        <code className="flex-1 break-all rounded-md border border-border bg-bg-base px-4 py-3 text-center font-mono text-lg font-semibold tracking-[0.1em] text-accent">
          {invite.code}
        </code>
        <CopyButton text={invite.code} />
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <ExpiryChip expiresAt={invite.expires_at} />
        <p className="flex-1 text-xs leading-relaxed text-text-muted">
          <Mail className="-mt-0.5 mr-1 inline size-3" />
          {t("admin.federation.invite.shareHint", {
            defaultValue:
              "Pásaselo al admin del otro servidor por chat encriptado o por teléfono. No lo publiques: cualquiera que lo tenga puede emparejarse.",
          })}
        </p>
      </div>
    </div>
  );
}

// ExpiryChip — contador de tiempo restante con severidad de color.
//   - verde   si quedan >= 12h
//   - ambar   si quedan 1-12h
//   - rojo    si quedan < 1h
// Se actualiza cada minuto via useEffect (no necesitamos segundos —
// los invites son de 24h, el sub-minuto no aporta y haría re-render
// constante). Cuando expira (< 0) muestra "expirado" en rojo: el
// backend lo descartara en el siguiente listing pero entre uno y
// otro la UI lo etiqueta honestamente.
function ExpiryChip({ expiresAt }: { expiresAt: string }) {
  const { t } = useTranslation();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);
  const expiresMs = new Date(expiresAt).getTime();
  const remainingMs = expiresMs - now;
  const expired = remainingMs <= 0;

  if (expired) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-error/10 px-2.5 py-0.5 text-[11px] font-medium text-error">
        <AlertCircle className="size-3" />
        {t("admin.federation.invite.expired", {
          defaultValue: "Expirado",
        })}
      </span>
    );
  }

  const remainingHours = remainingMs / (60 * 60 * 1000);
  let severity: "ok" | "warn" | "urgent";
  if (remainingHours >= 12) severity = "ok";
  else if (remainingHours >= 1) severity = "warn";
  else severity = "urgent";

  const cls = {
    ok: "bg-success/10 text-success",
    warn: "bg-warning/10 text-warning",
    urgent: "bg-error/10 text-error",
  }[severity];

  return (
    <span
      className={[
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-medium",
        cls,
      ].join(" ")}
      title={new Date(expiresAt).toLocaleString()}
    >
      <Clock className="size-3" />
      {formatRemaining(remainingMs, t)}
    </span>
  );
}

// sortByExpiry — el más reciente primero. "Más reciente" = más
// tiempo de vida útil restante; los invites tienen TTL idéntico
// (24h) así que esto equivale a "creado más recientemente".
function sortByExpiry(invs: FederationInvite[]): FederationInvite[] {
  return invs.toSorted(
    (a, b) => new Date(b.expires_at).getTime() - new Date(a.expires_at).getTime(),
  );
}

// formatRemaining devuelve "23h 47m" / "47m" / "5m" según orden de
// magnitud. Mantiene corto porque va en un chip.
function formatRemaining(
  ms: number,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  const totalMin = Math.floor(ms / 60_000);
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  if (h >= 1) {
    return t("admin.federation.invite.remainingH", {
      defaultValue: "{{h}}h {{m}}m",
      h,
      m,
    });
  }
  return t("admin.federation.invite.remainingM", {
    defaultValue: "{{m}}m",
    m: Math.max(1, m),
  });
}
