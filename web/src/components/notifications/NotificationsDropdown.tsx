import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { Check, CheckCheck, Handshake, Inbox, X } from "lucide-react";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
} from "@/api/hooks/notifications";
import type { AppNotification, NotificationKind } from "@/api/types";

// NotificationsDropdown — panel desplegable que pinta hasta 10
// notificaciones (las mas recientes; el limit del fetch ya las
// trae ordenadas). Acciones:
//   - Click en la entrada → marcar leida + navegar al link.
//   - Boton "Marcar todas como leidas" → bulk action.
//   - Boton X en cada entrada → marcar leida sin navegar.

interface Props {
  notifications: AppNotification[];
  unreadCount: number;
  onClose: () => void;
}

export function NotificationsDropdown({
  notifications,
  unreadCount,
  onClose,
}: Props) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const markRead = useMarkNotificationRead();
  const markAllRead = useMarkAllNotificationsRead();

  // Solo mostramos las 10 mas recientes en el dropdown. Si el user
  // quiere ver mas, el link "Ver todas" lleva a... bueno, todavia
  // no hay pagina full, lo dejamos para futuro.
  const visible = notifications.slice(0, 10);

  function handleEntryClick(n: AppNotification) {
    if (!n.read_at) {
      markRead.mutate(n.id);
    }
    if (n.link) {
      navigate(n.link);
      onClose();
    }
  }

  function handleMarkSingleRead(e: React.MouseEvent, n: AppNotification) {
    e.stopPropagation();
    if (!n.read_at) {
      markRead.mutate(n.id);
    }
  }

  return (
    <div
      role="menu"
      className="w-[360px] max-h-[calc(100vh-100px)] flex flex-col rounded-lg border border-border bg-bg-elevated shadow-2xl overflow-hidden"
    >
      <header className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold text-text-primary">
            {t("notifications.title", { defaultValue: "Notificaciones" })}
          </h3>
          {unreadCount > 0 && (
            <span className="rounded-full bg-accent/15 text-accent text-[10px] font-bold px-1.5 py-0.5">
              {unreadCount}
            </span>
          )}
        </div>
        {unreadCount > 0 && (
          <button
            type="button"
            onClick={() => markAllRead.mutate()}
            disabled={markAllRead.isPending}
            className="inline-flex items-center gap-1 text-xs text-text-muted hover:text-text-primary transition-colors disabled:opacity-50"
          >
            <CheckCheck className="size-3.5" />
            {t("notifications.markAllRead", {
              defaultValue: "Marcar todas leídas",
            })}
          </button>
        )}
      </header>

      <div className="flex-1 overflow-y-auto">
        {visible.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-6 py-10 text-center text-text-muted">
            <Inbox className="size-8" />
            <p className="text-sm">
              {t("notifications.empty", {
                defaultValue: "No tienes notificaciones",
              })}
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-border-subtle">
            {visible.map((n) => (
              <li key={n.id}>
                <button
                  type="button"
                  onClick={() => handleEntryClick(n)}
                  className={[
                    "w-full text-left px-4 py-3 transition-colors",
                    "hover:bg-bg-base",
                    !n.read_at ? "bg-accent/[0.04]" : "",
                  ].join(" ")}
                >
                  <div className="flex items-start gap-3">
                    <NotificationIcon kind={n.kind} unread={!n.read_at} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <p
                          className={[
                            "text-sm leading-snug",
                            n.read_at
                              ? "text-text-secondary"
                              : "text-text-primary font-medium",
                          ].join(" ")}
                        >
                          {n.title}
                        </p>
                        {!n.read_at && (
                          <button
                            type="button"
                            onClick={(e) => handleMarkSingleRead(e, n)}
                            aria-label={t("notifications.markRead", {
                              defaultValue: "Marcar como leída",
                            })}
                            className="flex-none p-1 rounded text-text-muted hover:text-text-primary hover:bg-bg-hover transition-colors"
                          >
                            <X className="size-3" />
                          </button>
                        )}
                        {n.read_at && (
                          <Check className="flex-none size-3 text-text-muted/60 mt-1" />
                        )}
                      </div>
                      {n.body && (
                        <p className="mt-0.5 text-xs leading-relaxed text-text-muted line-clamp-2">
                          {n.body}
                        </p>
                      )}
                      <p className="mt-1 text-[10px] text-text-muted/70">
                        {formatRelative(n.created_at, t)}
                      </p>
                    </div>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {notifications.length > 0 && (
        <footer className="px-4 py-2 border-t border-border-subtle text-center">
          <Link
            to="/me/notifications"
            onClick={onClose}
            className="text-xs text-accent hover:underline"
          >
            {notifications.length > 10
              ? t("notifications.viewAllWithCount", {
                  defaultValue: "Ver todas ({{n}})",
                  n: notifications.length,
                })
              : t("notifications.viewAll", { defaultValue: "Ver todas" })}
          </Link>
        </footer>
      )}
    </div>
  );
}

// NotificationIcon escoge un icono segun el kind. Switch sobre los
// kinds conocidos; default es Inbox neutro para kinds nuevos que el
// frontend aun no conozca (forward-compatible).
function NotificationIcon({
  kind,
  unread,
}: {
  kind: NotificationKind;
  unread: boolean;
}) {
  let Icon = Inbox;
  let color = "text-text-muted";
  switch (kind) {
    case "federation.pairing_request_received":
      Icon = Handshake;
      color = unread ? "text-accent" : "text-text-muted";
      break;
    case "federation.pairing_request_accepted":
      Icon = Handshake;
      color = unread ? "text-success" : "text-text-muted";
      break;
    case "federation.pairing_request_declined":
      Icon = Handshake;
      color = unread ? "text-warning" : "text-text-muted";
      break;
  }
  return (
    <div
      className={[
        "flex-none flex items-center justify-center size-8 rounded-full bg-bg-base",
        color,
      ].join(" ")}
    >
      <Icon className="size-4" />
    </div>
  );
}

// formatRelative simple: "ahora" / "Nm" / "Nh" / "Nd". El dropdown
// es efimero - cualquier user que quiera precision puede ver el
// timestamp en la pagina detallada (futura).
function formatRelative(
  iso: string,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return iso;
  const ageMs = Date.now() - ts;
  const ageMin = Math.floor(ageMs / 60_000);
  if (ageMin < 1) return t("notifications.now", { defaultValue: "ahora" });
  if (ageMin < 60)
    return t("notifications.minAgo", {
      defaultValue: "hace {{n}}m",
      n: ageMin,
    });
  const ageH = Math.floor(ageMin / 60);
  if (ageH < 24)
    return t("notifications.hourAgo", {
      defaultValue: "hace {{n}}h",
      n: ageH,
    });
  const ageD = Math.floor(ageH / 24);
  return t("notifications.dayAgo", {
    defaultValue: "hace {{n}}d",
    n: ageD,
  });
}
