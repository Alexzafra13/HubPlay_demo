import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  Clock,
  Fingerprint,
  Inbox,
  Volume2,
  X,
} from "lucide-react";
import {
  useAcceptPairingRequest,
  useCancelPairingRequest,
  useDeclinePairingRequest,
  usePairingRequests,
} from "@/api/hooks/federation";
import { Button, Spinner, UserAvatar } from "@/components/common";
import type { FederationPendingRequest } from "@/api/types";
import { ErrorBanner } from "./_shared";

// PendingRequestsSection — listado de peticiones de emparejamiento
// pendientes, ambas direcciones:
//
//   - incoming (alguien nos quiere emparejar): el admin compara
//     huella/palabras out-of-band y pulsa Aceptar o Rechazar.
//
//   - outgoing (le enviamos peticion): esperando respuesta. El admin
//     puede cancelar la propia mientras tanto.
//
// Estados terminales (accepted/declined/cancelled/expired) NO se
// listan aqui - el flujo termina con el peer creado (incoming
// accepted) o con la peticion descartada. Si el admin quiere ver el
// historial, /admin/peers/pairing-requests?status=all (futuro).

export function PendingRequestsSection() {
  const { t } = useTranslation();
  const requests = usePairingRequests();

  if (requests.isLoading) {
    return <Spinner />;
  }
  if (requests.error) {
    return <ErrorBanner message={String(requests.error)} />;
  }

  const all = requests.data ?? [];
  const active = all.filter((r) => r.status === "pending");
  const incoming = active.filter((r) => r.direction === "incoming");
  const outgoing = active.filter((r) => r.direction === "outgoing");

  if (active.length === 0) {
    // Empty state friendly — explica que hay aqui sin gritar "0 cosas".
    return (
      <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-6 py-10 text-center">
        <div className="rounded-full bg-bg-base p-3 text-text-muted">
          <Inbox className="size-6" />
        </div>
        <div>
          <p className="text-sm font-medium text-text-primary">
            {t("admin.federation.pending.emptyTitle", {
              defaultValue: "No hay peticiones pendientes",
            })}
          </p>
          <p className="mt-1 max-w-md text-xs text-text-muted">
            {t("admin.federation.pending.emptyHint", {
              defaultValue:
                "Cuando envíes una petición o recibas una de otro servidor aparecerá aquí.",
            })}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {incoming.length > 0 && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <ArrowDownToLine className="size-4 text-accent" />
            <h4 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
              {t("admin.federation.pending.incomingHeading", {
                defaultValue: "Peticiones recibidas",
              })}
            </h4>
            <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-bold text-accent">
              {incoming.length}
            </span>
          </div>
          <ul className="flex flex-col gap-2">
            {incoming.map((r) => (
              <IncomingRequestRow key={r.id} request={r} />
            ))}
          </ul>
        </div>
      )}

      {outgoing.length > 0 && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <ArrowUpFromLine className="size-4 text-text-muted" />
            <h4 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
              {t("admin.federation.pending.outgoingHeading", {
                defaultValue: "Peticiones enviadas",
              })}
            </h4>
          </div>
          <ul className="flex flex-col gap-2">
            {outgoing.map((r) => (
              <OutgoingRequestRow key={r.id} request={r} />
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function IncomingRequestRow({ request }: { request: FederationPendingRequest }) {
  const { t } = useTranslation();
  const accept = useAcceptPairingRequest();
  const decline = useDeclinePairingRequest();
  const [expanded, setExpanded] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function handleAccept() {
    setError(null);
    accept.mutate(request.id, {
      onError: (err) => setError(err.message),
    });
  }

  function handleDecline() {
    setError(null);
    if (
      !window.confirm(
        t("admin.federation.pending.declineConfirm", {
          defaultValue: "¿Rechazar la petición de {{name}}?",
          name: request.peer_name,
        }),
      )
    ) {
      return;
    }
    decline.mutate(request.id, {
      onError: (err) => setError(err.message),
    });
  }

  return (
    <li className="rounded-md border border-accent/30 bg-accent/5">
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <UserAvatar
          user={{
            username: request.peer_server_uuid || request.peer_name,
            display_name: request.peer_name,
            avatar_color: request.peer_avatar_color,
            avatar_image_url: request.peer_avatar_image_url ?? null,
          }}
          size="md"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-text-primary">
            {request.peer_name}
          </p>
          <p className="mt-0.5 truncate text-xs text-text-muted">
            {request.peer_base_url}
          </p>
        </div>
        <div className="flex flex-none flex-wrap gap-2">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => setExpanded((e) => !e)}
          >
            <Fingerprint className="mr-1.5 size-3.5" />
            {expanded
              ? t("admin.federation.pending.hideFingerprint", {
                  defaultValue: "Ocultar huella",
                })
              : t("admin.federation.pending.showFingerprint", {
                  defaultValue: "Ver huella",
                })}
          </Button>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={handleDecline}
            disabled={decline.isPending || accept.isPending}
          >
            <X className="mr-1 size-3.5" />
            {t("admin.federation.pending.decline", {
              defaultValue: "Rechazar",
            })}
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={handleAccept}
            isLoading={accept.isPending}
            disabled={decline.isPending}
          >
            <Check className="mr-1 size-3.5" />
            {t("admin.federation.pending.accept", {
              defaultValue: "Aceptar",
            })}
          </Button>
        </div>
      </div>

      {expanded && (
        <div className="border-t border-accent/20 px-4 py-3">
          <p className="mb-2 text-xs leading-relaxed text-text-muted">
            {t("admin.federation.pending.verifyHint", {
              defaultValue:
                "Compara estos valores con el otro admin por chat encriptado o teléfono. Solo cuando coincidan exactamente, acepta.",
            })}
          </p>
          <div className="rounded-md border border-border bg-bg-base px-3 py-2">
            <code className="block break-all text-center font-mono text-base tracking-[0.15em] text-accent">
              {request.fingerprint}
            </code>
          </div>
          <div className="mt-2 flex flex-wrap gap-2">
            <Volume2 className="size-3.5 self-center text-text-muted" />
            {request.fingerprint_words.map((w) => (
              <span
                key={w}
                className="rounded-md border border-border bg-bg-base px-2.5 py-1 font-mono text-xs font-semibold text-text-primary"
              >
                {w}
              </span>
            ))}
          </div>
        </div>
      )}

      {error && (
        <p className="px-4 pb-2 text-xs text-error">{error}</p>
      )}
    </li>
  );
}

function OutgoingRequestRow({ request }: { request: FederationPendingRequest }) {
  const { t } = useTranslation();
  const cancel = useCancelPairingRequest();
  const [error, setError] = useState<string | null>(null);

  function handleCancel() {
    setError(null);
    if (
      !window.confirm(
        t("admin.federation.pending.cancelConfirm", {
          defaultValue: "¿Cancelar la petición enviada a {{name}}?",
          name: request.peer_name,
        }),
      )
    ) {
      return;
    }
    cancel.mutate(request.id, {
      onError: (err) => setError(err.message),
    });
  }

  // Tiempo restante (rough): chip con horas/dias hasta expires_at.
  // No usamos contador en vivo aqui - los pairing requests son de 7
  // dias, el sub-minuto no aporta UI y reduce noise.
  const remaining = expiryLabel(request.expires_at, t);

  return (
    <li className="rounded-md border border-border bg-bg-base">
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <UserAvatar
          user={{
            username: request.peer_server_uuid || request.peer_name,
            display_name: request.peer_name,
            avatar_color: request.peer_avatar_color,
            avatar_image_url: request.peer_avatar_image_url ?? null,
          }}
          size="md"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-text-primary">
            {request.peer_name}
          </p>
          <p className="mt-0.5 truncate text-xs text-text-muted">
            {request.peer_base_url}
          </p>
        </div>
        <span className="inline-flex items-center gap-1 rounded-full bg-bg-elevated px-2 py-0.5 text-[11px] text-text-muted">
          <Clock className="size-3" />
          {remaining}
        </span>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={handleCancel}
          disabled={cancel.isPending}
        >
          <X className="mr-1 size-3.5" />
          {t("admin.federation.pending.cancel", {
            defaultValue: "Cancelar",
          })}
        </Button>
      </div>
      {error && <p className="px-4 pb-2 text-xs text-error">{error}</p>}
    </li>
  );
}

function expiryLabel(
  iso: string,
  t: (k: string, opts?: Record<string, unknown>) => string,
): string {
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return iso;
  const ms = ts - Date.now();
  if (ms <= 0) {
    return t("admin.federation.pending.expired", { defaultValue: "Expirada" });
  }
  const days = Math.floor(ms / (24 * 60 * 60 * 1000));
  if (days >= 1) {
    return t("admin.federation.pending.expiresInDays", {
      defaultValue: "expira en {{n}}d",
      n: days,
    });
  }
  const hours = Math.max(1, Math.floor(ms / (60 * 60 * 1000)));
  return t("admin.federation.pending.expiresInHours", {
    defaultValue: "expira en {{n}}h",
    n: hours,
  });
}
