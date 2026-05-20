import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import {
  Bell,
  Check,
  CheckCheck,
  Handshake,
  Inbox,
  X,
} from "lucide-react";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useMyNotifications,
} from "@/api/hooks/notifications";
import { Spinner } from "@/components/common";
import type { AppNotification, NotificationKind } from "@/api/types";

// MyNotifications — /me/notifications. Pagina completa del inbox.
//
// Comparado con el dropdown:
//   - Sin cap de 10 entradas.
//   - Chips "Todas / No leidas" para filtrar.
//   - "Marcar todas como leidas" prominente cuando hay no-leidas.
//   - Empty state amigable cuando no hay nada (no el "tag null" del
//     dropdown - aqui es una pagina, hay que pintarla incluso vacia).
//
// Carga lazy desde App.tsx para no inflar el bundle base.

type Filter = "all" | "unread";

export default function MyNotifications() {
  const { t } = useTranslation();
  const { data, isLoading } = useMyNotifications();
  const markAllRead = useMarkAllNotificationsRead();
  const [filter, setFilter] = useState<Filter>("all");

  const unreadCount = data?.unread_count ?? 0;
  const totalCount = data?.data?.length ?? 0;

  const visible = useMemo(() => {
    const all = data?.data ?? [];
    if (filter === "unread") return all.filter((n) => !n.read_at);
    return all;
  }, [data, filter]);

  if (isLoading && !data) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <Spinner />
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 p-4 pt-6 sm:p-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="rounded-lg bg-accent/10 p-2.5 text-accent">
            <Bell className="size-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-text-primary sm:text-2xl">
              {t("notifications.title", { defaultValue: "Notificaciones" })}
            </h1>
            <p className="mt-0.5 text-xs text-text-muted">
              {unreadCount > 0
                ? t("notifications.unreadCount", {
                    defaultValue: "{{n}} sin leer",
                    n: unreadCount,
                  })
                : t("notifications.allRead", {
                    defaultValue: "Todo al día",
                  })}
            </p>
          </div>
        </div>
        {unreadCount > 0 && (
          <button
            type="button"
            onClick={() => markAllRead.mutate()}
            disabled={markAllRead.isPending}
            className="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-xs font-medium text-text-secondary transition-colors hover:bg-bg-hover hover:text-text-primary disabled:opacity-50"
          >
            <CheckCheck className="size-3.5" />
            {t("notifications.markAllRead", {
              defaultValue: "Marcar todas leídas",
            })}
          </button>
        )}
      </header>

      <div className="flex items-center gap-2">
        <FilterChip
          active={filter === "all"}
          onClick={() => setFilter("all")}
          label={t("notifications.filter.all", { defaultValue: "Todas" })}
          count={totalCount}
        />
        <FilterChip
          active={filter === "unread"}
          onClick={() => setFilter("unread")}
          label={t("notifications.filter.unread", {
            defaultValue: "No leídas",
          })}
          count={unreadCount}
        />
      </div>

      {visible.length === 0 ? (
        <EmptyState filter={filter} />
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((n) => (
            <NotificationRow key={n.id} notif={n} />
          ))}
        </ul>
      )}
    </div>
  );
}

function FilterChip({
  active,
  onClick,
  label,
  count,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
        active
          ? "border-accent bg-accent/10 text-accent"
          : "border-border bg-bg-elevated text-text-secondary hover:border-border-strong hover:text-text-primary",
      ].join(" ")}
      aria-pressed={active}
    >
      {label}
      <span
        className={[
          "rounded-full px-1.5 text-[10px] font-bold",
          active ? "bg-accent/20" : "bg-bg-base text-text-muted",
        ].join(" ")}
      >
        {count}
      </span>
    </button>
  );
}

function NotificationRow({ notif }: { notif: AppNotification }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const markRead = useMarkNotificationRead();

  function handleClick() {
    if (!notif.read_at) {
      markRead.mutate(notif.id);
    }
    if (notif.link) {
      navigate(notif.link);
    }
  }

  function handleMarkRead(e: React.MouseEvent) {
    e.stopPropagation();
    if (!notif.read_at) {
      markRead.mutate(notif.id);
    }
  }

  return (
    <li>
      <button
        type="button"
        onClick={handleClick}
        className={[
          "flex w-full items-start gap-3 rounded-md border px-4 py-3 text-left transition-colors",
          notif.read_at
            ? "border-border-subtle bg-bg-elevated hover:bg-bg-hover"
            : "border-accent/30 bg-accent/[0.04] hover:bg-accent/[0.06]",
        ].join(" ")}
      >
        <NotificationIcon kind={notif.kind} unread={!notif.read_at} />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-2">
            <p
              className={[
                "text-sm leading-snug",
                notif.read_at
                  ? "text-text-secondary"
                  : "text-text-primary font-medium",
              ].join(" ")}
            >
              {notif.title}
            </p>
            {!notif.read_at ? (
              <button
                type="button"
                onClick={handleMarkRead}
                aria-label={t("notifications.markRead", {
                  defaultValue: "Marcar como leída",
                })}
                className="flex-none rounded p-1 text-text-muted hover:bg-bg-hover hover:text-text-primary"
              >
                <X className="size-3.5" />
              </button>
            ) : (
              <Check className="mt-0.5 size-3.5 flex-none text-text-muted/60" />
            )}
          </div>
          {notif.body && (
            <p className="mt-1 text-xs leading-relaxed text-text-muted">
              {notif.body}
            </p>
          )}
          <p className="mt-1.5 text-[10px] text-text-muted/70">
            {new Date(notif.created_at).toLocaleString()}
          </p>
        </div>
      </button>
    </li>
  );
}

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
        "flex size-9 flex-none items-center justify-center rounded-full bg-bg-base",
        color,
      ].join(" ")}
    >
      <Icon className="size-4" />
    </div>
  );
}

function EmptyState({ filter }: { filter: Filter }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-6 py-16 text-center">
      <div className="rounded-full bg-bg-base p-4 text-text-muted">
        <Inbox className="size-8" />
      </div>
      <p className="text-sm font-medium text-text-primary">
        {filter === "unread"
          ? t("notifications.emptyUnreadTitle", {
              defaultValue: "Todo al día",
            })
          : t("notifications.emptyAllTitle", {
              defaultValue: "Aún no tienes notificaciones",
            })}
      </p>
      <p className="max-w-sm text-xs leading-relaxed text-text-muted">
        {filter === "unread"
          ? t("notifications.emptyUnreadHint", {
              defaultValue:
                "No hay nada nuevo. Cuando ocurra algo importante aparecerá aquí.",
            })
          : t("notifications.emptyAllHint", {
              defaultValue:
                "Cuando recibas peticiones de emparejamiento o eventos del sistema, las verás listadas aquí.",
            })}
      </p>
      <Link
        to="/"
        className="mt-2 text-xs text-accent hover:underline"
      >
        {t("notifications.backHome", { defaultValue: "Volver al inicio" })}
      </Link>
    </div>
  );
}
