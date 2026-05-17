import type { User } from "@/api/types";
import { avatarColorForUser } from "@/utils/avatarColor";
import { getInitials } from "@/utils/userDisplay";

export type UserAvatarSize = "xs" | "sm" | "md" | "lg" | "xl";

export interface UserAvatarProps {
  user:
    | Pick<User, "username" | "display_name" | "avatar_color">
    | null
    | undefined;
  size?: UserAvatarSize;
  className?: string;
  // Decorativo por defecto; quien lo use con título propio (nombre al
  // lado) no necesita anuncio adicional. Pasa `label` si el avatar
  // aparece sólo, sin etiqueta visible.
  label?: string;
}

// Tamaños en px del círculo y de la tipografía. Mantengo la escala
// alineada con la que ya usaban TopBar (h-9 w-9 = 36) y los chips
// de cabecera de WhoIsWatching (h-12 = 48), para no introducir un
// tercer set.
const SIZE_CLASS: Record<UserAvatarSize, string> = {
  xs: "h-6 w-6 text-[9px]",
  sm: "h-7 w-7 text-[10px]",
  md: "h-9 w-9 text-[12px]",
  lg: "h-12 w-12 text-sm",
  xl: "h-16 w-16 text-base",
};

// Componente único para los círculos de iniciales que antes estaban
// duplicados en TopBar, MobileDrawer, WhoIsWatching y la tabla
// admin. La cadena es: avatar_color override → color FNV del
// username → fallback de paleta. Cuando exista imagen subida (Fase
// 2) se añadirá una rama `src` aquí mismo.
export function UserAvatar({
  user,
  size = "md",
  className,
  label,
}: UserAvatarProps) {
  const palette = avatarColorForUser(user);
  const initials = getInitials(user);
  return (
    <span
      className={[
        "inline-flex items-center justify-center rounded-full font-semibold text-white ring-1 ring-white/15",
        SIZE_CLASS[size],
        className ?? "",
      ].join(" ")}
      style={{ background: palette.background }}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {initials}
    </span>
  );
}
