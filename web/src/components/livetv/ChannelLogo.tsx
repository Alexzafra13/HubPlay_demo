import { useState } from "react";

/**
 * ChannelLogo renders the channel's logo with a graceful fallback to the
 * channel number when the image fails to load OR is missing entirely.
 *
 * Previously the channel grid rendered <img src={channel.logo_url}> with no
 * onError handler — a 404 left an empty slot. This component covers both
 * "no URL" and "URL present but dead".
 */
interface ChannelLogoProps {
  logoUrl?: string | null;
  number: number;
  name: string;
  sizeClassName: string; // e.g. "w-7 h-7"
  fallbackTextClassName: string; // e.g. "text-sm font-bold"
  alt?: string;
}

export function ChannelLogo({
  logoUrl,
  number,
  name,
  sizeClassName,
  fallbackTextClassName,
  alt,
}: ChannelLogoProps) {
  const [failed, setFailed] = useState(false);
  const showImage = logoUrl && !failed;

  if (showImage) {
    return (
      <img
        src={logoUrl}
        alt={alt ?? name}
        className={`${sizeClassName} object-contain`}
        loading="lazy"
        onError={() => setFailed(true)}
      />
    );
  }
  return (
    <span
      className={`${sizeClassName} rounded flex items-center justify-center text-text-muted ${fallbackTextClassName}`}
      aria-label={name}
    >
      {number || "?"}
    </span>
  );
}
