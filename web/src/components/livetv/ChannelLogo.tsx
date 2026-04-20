import { useState } from "react";

/**
 * ChannelLogo renders a channel's upstream logo with a deterministic
 * initial-letter fallback when the image is missing, broken, or slow to
 * load. The fallback values (initials, bg, fg) are computed server-side
 * in `iptv.DeriveLogoFallback` so the same channel always looks the same
 * — across renders, sessions, and clients.
 *
 * Sizing is driven from the outside via `className` so parents control
 * their layout. The component only guarantees it fills the box, centers
 * the fallback text, and falls back on error.
 */
interface ChannelLogoProps {
  logoUrl?: string | null;
  initials: string;
  bg: string;
  fg: string;
  name: string;
  /** Tailwind classes for outer sizing + optional rounding, e.g. "w-10 h-10 rounded-lg". */
  className?: string;
  /** Font sizing for the initials text, e.g. "text-sm font-bold". */
  textClassName?: string;
}

export function ChannelLogo({
  logoUrl,
  initials,
  bg,
  fg,
  name,
  className = "w-10 h-10 rounded-lg",
  textClassName = "text-xs font-bold",
}: ChannelLogoProps) {
  const [failed, setFailed] = useState(false);
  const showImage = !!logoUrl && !failed;

  // The background + initials always render — the <img> layers on top when
  // available. On error the image hides itself and the fallback is already
  // painted, so there's no flash / layout shift.
  return (
    <div
      className={`relative flex items-center justify-center overflow-hidden ${className}`}
      style={{ backgroundColor: bg, color: fg }}
      aria-label={name}
    >
      <span className={textClassName} aria-hidden={showImage}>
        {initials}
      </span>
      {showImage && (
        <img
          src={logoUrl}
          alt=""
          className="absolute inset-0 h-full w-full object-contain p-1"
          loading="lazy"
          onError={() => setFailed(true)}
        />
      )}
    </div>
  );
}
