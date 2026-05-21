import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { m, AnimatePresence } from "framer-motion";
import { Bell } from "lucide-react";
import { useMyNotifications } from "@/api/hooks/notifications";
import { NotificationsDropdown } from "./NotificationsDropdown";

// NotificationsBell — botón con icono Bell que vive en TopBar.
//
// El cliente quiso explicitamente: "si no hay no aparezca nada". El
// componente devuelve null cuando unread_count === 0 (y nada esta
// loading/error). El dropdown solo se abre al click y carga al
// instante porque la query ya esta hidratada.
//
// Se monta para todos los usuarios autenticados, no solo admins, porque
// el sistema de notifications es generico - los pairing requests son
// admin-target, pero más adelante habrá scan-completed / "amigo
// empezo a ver X" / etc. para usuarios normales.
export function NotificationsBell() {
  const { t } = useTranslation();
  const { data, isLoading } = useMyNotifications();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Cerrar al click fuera del dropdown.
  useEffect(() => {
    if (!open) return;
    function handleClickOutside(e: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [open]);

  // ESC cierra (a11y).
  useEffect(() => {
    if (!open) return;
    function handleEsc(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("keydown", handleEsc);
    return () => document.removeEventListener("keydown", handleEsc);
  }, [open]);

  // Mientras carga la primera vez (sin datos previos), no pintar nada
  // — preferimos ocultar el bell hasta saber si hay unread que mostrar
  // un placeholder vacio que despues desaparezca de golpe.
  if (isLoading && !data) return null;

  const unread = data?.unread_count ?? 0;
  const hasAny = (data?.data?.length ?? 0) > 0;

  // Si nunca ha llegado nada al inbox y no hay nada pending de leer,
  // ocultamos el bell completo - el user pidio explicitamente "que
  // no aparezca nada".
  if (unread === 0 && !hasAny) return null;

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-label={
          unread > 0
            ? t("notifications.titleWithCount", {
                defaultValue: "Notificaciones, {{n}} sin leer",
                n: unread,
              })
            : t("notifications.title", {
                defaultValue: "Notificaciones",
              })
        }
        aria-expanded={open}
        aria-haspopup="menu"
        className={[
          "relative flex items-center justify-center size-10 rounded-lg",
          "text-text-secondary hover:text-text-primary hover:bg-bg-hover",
          "transition-colors",
          open ? "bg-bg-hover text-text-primary" : "",
        ].join(" ")}
      >
        <Bell className="size-[19px]" strokeWidth={1.7} />
        {unread > 0 && (
          <span
            aria-hidden
            className="absolute top-1.5 right-1.5 min-w-[18px] h-[18px] px-1 flex items-center justify-center rounded-full bg-accent text-bg-base text-[10px] font-bold leading-none ring-2 ring-bg-base"
          >
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      <AnimatePresence>
        {open && (
          <m.div
            initial={{ opacity: 0, y: -4, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -4, scale: 0.98 }}
            transition={{ duration: 0.12 }}
            className="absolute right-0 top-full mt-2 z-50"
          >
            <NotificationsDropdown
              notifications={data?.data ?? []}
              unreadCount={unread}
              onClose={() => setOpen(false)}
            />
          </m.div>
        )}
      </AnimatePresence>
    </div>
  );
}
