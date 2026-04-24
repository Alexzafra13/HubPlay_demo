import { useTranslation } from "react-i18next";
import type { Channel } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { capitalize } from "./epgHelpers";

interface OverlayHeaderProps {
  channel: Channel;
  isFavorite?: boolean;
  onToggleFavorite?: () => void;
  onClose: () => void;
}

/**
 * OverlayHeader — top bar of the PlayerOverlay.
 *
 * Left: close button (sends the user back to Discover/Guide/Favorites).
 * Middle: channel identity — logo, CH number, name, live pill +
 *         category + country.
 * Right: favorite toggle (optional — only renders when both
 *         `isFavorite` and `onToggleFavorite` are provided).
 *
 * Purely presentational; all state (including which channel is
 * playing and whether it's favorited) comes in via props.
 */
export function OverlayHeader({
  channel,
  isFavorite = false,
  onToggleFavorite,
  onClose,
}: OverlayHeaderProps) {
  const { t } = useTranslation();
  return (
    <header className="flex items-center gap-3 border-b border-tv-line bg-tv-bg-0/90 px-3 py-3 md:px-5">
      <button
        type="button"
        onClick={onClose}
        aria-label={t("common.close", { defaultValue: "Cerrar" })}
        className="flex h-9 w-9 items-center justify-center rounded-full border border-tv-line text-tv-fg-1 transition-colors hover:bg-tv-bg-2 hover:text-tv-fg-0"
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <line x1="19" y1="12" x2="5" y2="12" />
          <polyline points="12 19 5 12 12 5" />
        </svg>
      </button>

      <ChannelLogo
        logoUrl={channel.logo_url}
        initials={channel.logo_initials}
        bg={channel.logo_bg}
        fg={channel.logo_fg}
        name={channel.name}
        className="h-10 w-10 rounded-tv-sm"
        textClassName="text-xs font-bold"
      />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-mono text-[10px] uppercase tracking-widest text-tv-fg-3">
            CH {channel.number}
          </span>
          <span className="truncate text-sm font-semibold text-tv-fg-0">
            {channel.name}
          </span>
        </div>
        <div className="mt-0.5 flex items-center gap-2">
          <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            Live
          </span>
          <span className="text-[11px] text-tv-fg-2">
            {capitalize(channel.category)}
            {channel.country ? ` · ${channel.country.toUpperCase()}` : ""}
          </span>
        </div>
      </div>

      {onToggleFavorite && (
        <button
          type="button"
          onClick={onToggleFavorite}
          aria-label={
            isFavorite
              ? t("liveTV.removeFromFavorites", {
                  defaultValue: "Quitar de favoritos",
                })
              : t("liveTV.addToFavorites", {
                  defaultValue: "Añadir a favoritos",
                })
          }
          aria-pressed={isFavorite}
          className={[
            "flex h-9 w-9 items-center justify-center rounded-full border border-tv-line transition-colors hover:bg-tv-bg-2",
            isFavorite ? "text-tv-live" : "text-tv-fg-1",
          ].join(" ")}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill={isFavorite ? "currentColor" : "none"}
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
          </svg>
        </button>
      )}
    </header>
  );
}
