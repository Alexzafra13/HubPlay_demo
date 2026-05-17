import { useEffect, useState } from "react";
import type { User } from "@/api/types";
import { avatarColorForUser } from "@/utils/avatarColor";
import { getInitials } from "@/utils/userDisplay";

export type UserAvatarSize = "xs" | "sm" | "md" | "lg" | "xl";

export interface UserAvatarProps {
  user:
    | Pick<User, "username" | "display_name" | "avatar_color" | "avatar_image_url">
    | null
    | undefined;
  size?: UserAvatarSize;
  className?: string;
  // Decorativo por defecto; quien lo use con título propio (nombre al
  // lado) no necesita anuncio adicional. Pasa `label` si el avatar
  // aparece sólo, sin etiqueta visible.
  label?: string;
  // Sobrescribe la URL de imagen del usuario para previews en vivo
  // (ej: mostrar lo que el uploader acaba de seleccionar antes de
  // enviarlo). Si null/'' explícitamente, suprime la imagen del
  // usuario y fuerza el fallback de iniciales.
  src?: string | null;
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

// Componente único para todos los avatares. Cadena de fallback:
//   1. `src` (override en vivo del uploader) si está definido.
//   2. `user.avatar_image_url` (imagen subida y guardada en backend).
//   3. iniciales sobre color (avatar_color override → FNV → paleta).
// Los dos primeros pintan <img>; el tercero pinta un span con texto.
// Esto evita el "flash" del fallback mientras la imagen carga porque
// no la tachamos hasta que onError dispara.
export function UserAvatar({
  user,
  size = "md",
  className,
  label,
  src,
}: UserAvatarProps) {
  const palette = avatarColorForUser(user);
  const initials = getInitials(user);
  const imageSrc =
    src !== undefined ? src : user?.avatar_image_url ?? null;

  // Si la <img> falla (URL 404, fichero borrado del disco, cache-buster
  // contra una versión inexistente...) caemos a iniciales en lugar de
  // dejar el círculo de color vacío. Reseteamos el flag cuando la URL
  // cambia para que un nuevo upload tenga otra oportunidad de cargar.
  const [broken, setBroken] = useState(false);
  useEffect(() => {
    setBroken(false);
  }, [imageSrc]);

  const base = [
    "inline-flex items-center justify-center overflow-hidden rounded-full font-semibold text-white ring-1 ring-white/15 select-none",
    SIZE_CLASS[size],
    className ?? "",
  ].join(" ");

  const showImage = !!imageSrc && !broken;

  return (
    <span
      className={base}
      style={{ background: palette.background }}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {showImage ? (
        <img
          src={imageSrc}
          alt=""
          className="h-full w-full object-cover"
          draggable={false}
          onError={() => setBroken(true)}
        />
      ) : (
        initials
      )}
    </span>
  );
}
