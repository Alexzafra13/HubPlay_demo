import { useState } from "react";
import type { Person } from "@/api/types";

// Plex-style cast/crew chip: a generous circular avatar stacked over
// the name and the character/role line. No card chrome around it —
// the avatar IS the frame, and the surrounding hero/page tint reads
// through. Failed photo loads (broken URL, 404 from the people thumb
// endpoint) flip to an initial-letter placeholder via `onError`; the
// failure state keys off the URL so a re-fetch with a new URL retries
// instead of inheriting the previous failure.
export function CastChip({ person }: { person: Person }) {
  const [failedUrl, setFailedUrl] = useState<string | null>(null);
  const showImage = !!person.image_url && failedUrl !== person.image_url;
  // Actor entries put the character name on the second line; crew
  // entries (director, writer, producer) put the role label there
  // because the role IS the descriptor for them.
  const subtitle = person.character || person.role;

  return (
    <div className="flex w-[120px] flex-col items-center gap-2 text-center">
      <div className="flex h-24 w-24 shrink-0 items-center justify-center overflow-hidden rounded-full bg-bg-elevated text-xl font-bold text-text-muted ring-1 ring-border/40 sm:h-28 sm:w-28">
        {showImage ? (
          <img
            src={person.image_url}
            alt={person.name}
            loading="lazy"
            decoding="async"
            width={112}
            height={112}
            className="h-full w-full object-cover"
            onError={() => setFailedUrl(person.image_url ?? null)}
          />
        ) : (
          person.name.charAt(0)
        )}
      </div>
      <div className="flex flex-col gap-0.5">
        <span className="line-clamp-2 text-sm font-medium leading-snug text-text-primary">
          {person.name}
        </span>
        {subtitle && (
          <span className="line-clamp-2 text-xs leading-snug text-text-muted">
            {subtitle}
          </span>
        )}
      </div>
    </div>
  );
}
