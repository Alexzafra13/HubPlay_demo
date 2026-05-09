// WhoIsWatching — cinematic profile picker. Shown after login (and
// via TopBar "Cambiar perfil") whenever the current account has
// more than one profile in the tree (parent + ≥1 child). Solo
// accounts skip this screen entirely and go straight to /.
//
// Layout brief:
//
//   - "with content" (≥ 4 items in the catalogue have a poster):
//     two-column split. Left column owns the picker (title +
//     circular avatars + bottom rail). Right column is a visible
//     poster wall — actual posters at full opacity, not the
//     ambient blur-mosaic — so the page reads "this is YOUR
//     media platform" instead of "generic auth screen".
//
//   - "fresh install": single column, picker centred. The aurora
//     gradients carry the canvas alone; no empty wall on the
//     right. Same vibe as the Login page.
//
// Avatars are circular (matching the TopBar), generously sized,
// with a halo of the profile's palette colour bleeding behind
// on hover/focus. The hovered profile also tints the page-level
// ambient — a soft radial bleed in that colour follows the cursor.

import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";
import { LogOut, Pencil } from "lucide-react";
import { ArrowLeft } from "lucide-react";
import {
  useItems,
  useLogout,
  useMe,
  useProfiles,
  useSwitchProfile,
} from "@/api/hooks";
import type { MediaItem, ProfileSummary } from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Spinner } from "@/components/common";
import { avatarColorFor } from "@/utils/avatarColor";
import { getInitials } from "@/utils/userDisplay";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

// Minimum posters the catalogue must surface before we promote the
// "two-column with poster wall" layout. Below this, the wall would
// look sparse and we get a better-looking single-column instead.
const MIN_POSTERS_FOR_WALL = 4;

export default function WhoIsWatching() {
  const { t } = useTranslation();
  const { data: profiles, isLoading, error } = useProfiles();
  const { data: me } = useMe();
  const { user, refreshMe, logout: clearLocalAuth } = useAuthStore();
  const navigate = useNavigate();
  const switchProfile = useSwitchProfile();
  const logoutMutation = useLogout();

  const [selected, setSelected] = useState<ProfileSummary | null>(null);
  const [pin, setPin] = useState("");
  const [pinError, setPinError] = useState<string | null>(null);
  const [pinAttempting, setPinAttempting] = useState(false);
  const [hoveredProfileId, setHoveredProfileId] = useState<string | null>(null);

  // Catalogue slice for the right-side poster wall + the ambient
  // hover bleed. One fetch, two consumers. Failures are non-fatal:
  // the picker degrades to a single-column aurora layout.
  const { data: itemsData } = useItems(
    { limit: 12, sort_by: "date_added", sort_order: "desc" },
    { staleTime: 5 * 60 * 1000, retry: false },
  );
  const posters = useMemo(() => {
    const items = (itemsData?.items ?? []) as MediaItem[];
    return items
      .map((it) => ({ id: it.id, src: it.poster_url, title: it.title }))
      .filter((p): p is { id: string; src: string; title: string } =>
        Boolean(p.src),
      )
      .slice(0, 6);
  }, [itemsData]);

  const showWall = posters.length >= MIN_POSTERS_FOR_WALL;

  useEffect(() => {
    if (!isLoading && !error && profiles && profiles.length <= 1) {
      navigate("/", { replace: true });
    }
  }, [isLoading, error, profiles, navigate]);

  // The hovered profile drives the page-level ambient tint behind
  // the picker. Computed up here (before the early returns below)
  // because hooks must always run in the same order — moving this
  // useMemo past an `if (isLoading) return ...` would be a real
  // rules-of-hooks violation.
  const hoveredHex = useMemo(() => {
    if (!hoveredProfileId) return null;
    const p = profiles?.find((x) => x.id === hoveredProfileId);
    return p ? avatarColorFor(p.username).background : null;
  }, [hoveredProfileId, profiles]);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-bg-base">
        <Spinner size="lg" />
      </div>
    );
  }

  const profilesMissing = !profiles || profiles.length === 0;
  if (error || profilesMissing) {
    return (
      <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-bg-base px-6 py-12">
        <CinematicBackdrop hoveredHex={null} hasWall={false} />
        <div className="relative z-10 flex max-w-md flex-col items-center gap-5 text-center">
          <BrandWordmark height={48} className="opacity-90" />
          <h2 className="text-xl font-semibold text-text-primary">
            {t("whoIsWatching.loadFailedTitle", {
              defaultValue: "No pudimos cargar los perfiles",
            })}
          </h2>
          <p className="text-sm text-text-muted">
            {error
              ? error.message
              : t("whoIsWatching.loadFailedHint", {
                  defaultValue:
                    "El servidor no devolvió ningún perfil para esta cuenta. Pulsa reintentar; si persiste, cierra sesión y vuelve a entrar.",
                })}
          </p>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => navigate("/", { replace: true })}
              className="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-xs text-text-secondary backdrop-blur-md transition-all hover:border-white/20 hover:bg-white/10 hover:text-text-primary"
            >
              {t("whoIsWatching.continueAnyway", {
                defaultValue: "Continuar al inicio",
              })}
            </button>
            <button
              type="button"
              onClick={() => void handleSignOut()}
              className="inline-flex items-center gap-1.5 rounded-full border border-red-500/20 bg-red-500/5 px-4 py-2 text-xs text-red-400/85 backdrop-blur-md transition-all hover:border-red-500/40 hover:bg-red-500/10 hover:text-red-400"
            >
              <LogOut className="h-3.5 w-3.5" />
              {t("whoIsWatching.signOut", {
                defaultValue: "Cerrar sesión",
              })}
            </button>
          </div>
        </div>
      </div>
    );
  }

  async function pickProfile(profile: ProfileSummary) {
    setSelected(profile);
    setPin("");
    setPinError(null);
    if (!profile.has_pin) {
      await commitSwitch(profile, "");
    }
  }

  async function commitSwitch(profile: ProfileSummary, p: string) {
    setPinError(null);
    setPinAttempting(true);
    try {
      if (profile.id === user?.id) {
        navigate("/", { replace: true });
        return;
      }
      await switchProfile.mutateAsync({ profileId: profile.id, pin: p });
      await refreshMe?.();
      navigate("/", { replace: true });
    } catch {
      setPinError(t("whoIsWatching.errorWrongPin"));
      setPin("");
    } finally {
      setPinAttempting(false);
    }
  }

  async function handleSignOut() {
    try {
      await logoutMutation.mutateAsync();
    } finally {
      clearLocalAuth();
      navigate("/login", { replace: true });
    }
  }

  const canManage = me?.role === "admin";

  // The picker block (title + avatars + rail) is shared between
  // the single-column and split layouts. Pulled to a local
  // closure rather than a child component to keep the hover /
  // commit handlers in scope without prop-drilling.
  const renderPicker = (alignment: "center" | "start") => (
    <div
      className={[
        "flex flex-col",
        alignment === "center" ? "items-center text-center" : "items-start text-left",
      ].join(" ")}
    >
      <motion.h1
        initial={{ opacity: 0, y: -6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4, ease: [0.22, 0.61, 0.36, 1] }}
        className="mb-3 text-5xl font-extralight tracking-[-0.015em] text-text-primary sm:text-6xl"
      >
        {t("whoIsWatching.title")}
      </motion.h1>
      <motion.p
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.5, delay: 0.15 }}
        className="mb-12 text-sm tracking-wider text-text-muted"
      >
        {t("whoIsWatching.subtitle", {
          defaultValue: "Selecciona tu perfil para continuar.",
        })}
      </motion.p>
      <motion.div
        initial="hidden"
        animate="show"
        variants={{
          hidden: {},
          show: {
            transition: {
              staggerChildren: 0.08,
              delayChildren: 0.2,
            },
          },
        }}
        className={[
          // Stacked vertically when in the split-layout (alignment="start")
          // so the picker reads as a side rail next to the poster wall;
          // the centred fallback layout still wraps a horizontal row for
          // installations with no catalogue art on the right. This
          // matches the "list usuarios arriba a abajo" pattern HBO and
          // Disney+ use on TV-shaped surfaces, while the centred row
          // covers the desktop-like Netflix shape.
          alignment === "start"
            ? "flex flex-col gap-6"
            : "flex flex-wrap justify-center gap-8 sm:gap-10",
        ].join(" ")}
      >
        {profiles?.map((p) => (
          <ProfileCard
            key={p.id}
            profile={p}
            onClick={() => void pickProfile(p)}
            onHoverChange={(h) => setHoveredProfileId(h ? p.id : null)}
            compact={alignment === "start"}
          />
        ))}
      </motion.div>

      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.5, delay: 0.5 }}
        className={[
          "mt-12 flex flex-wrap items-center gap-3 text-xs",
          alignment === "center" ? "justify-center" : "justify-start",
        ].join(" ")}
      >
        {canManage && (
          <button
            type="button"
            onClick={() => navigate("/admin/users")}
            className="inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/5 px-4 py-2 text-text-secondary backdrop-blur-md transition-all hover:border-white/20 hover:bg-white/10 hover:text-text-primary"
          >
            <Pencil className="h-3.5 w-3.5" />
            {t("whoIsWatching.manageProfiles", {
              defaultValue: "Gestionar perfiles",
            })}
          </button>
        )}
        <button
          type="button"
          onClick={() => void handleSignOut()}
          className="inline-flex items-center gap-1.5 rounded-full border border-red-500/20 bg-red-500/5 px-4 py-2 text-red-400/85 backdrop-blur-md transition-all hover:border-red-500/40 hover:bg-red-500/10 hover:text-red-400"
        >
          <LogOut className="h-3.5 w-3.5" />
          {t("whoIsWatching.signOut", {
            defaultValue: "Cerrar sesión",
          })}
        </button>
      </motion.div>
    </div>
  );

  return (
    <div
      className="relative flex min-h-screen flex-col items-center overflow-hidden bg-bg-base px-6 py-10 sm:py-14"
      onMouseLeave={() => setHoveredProfileId(null)}
    >
      <CinematicBackdrop hoveredHex={hoveredHex} hasWall={showWall} />

      {/* Back button — top-left. The picker is reachable both from
          a fresh login (where "back" goes nowhere) AND from
          TopBar → Cambiar perfil (where "back" should drop the
          user back into the home shell on their current profile).
          We always navigate to "/" rather than navigate(-1):
          history-back from a fresh login lands on /login again,
          which traps the user. "/" is the right answer in both
          flows because ProtectedRoute will keep them
          authenticated. */}
      <motion.button
        type="button"
        onClick={() => navigate("/")}
        initial={{ opacity: 0, x: -8 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.4 }}
        className="absolute left-4 top-4 z-20 inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-text-secondary backdrop-blur-md transition-all hover:border-white/20 hover:bg-white/10 hover:text-text-primary sm:left-6 sm:top-6"
        aria-label={t("whoIsWatching.back", { defaultValue: "Volver" })}
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        {t("whoIsWatching.back", { defaultValue: "Volver" })}
      </motion.button>

      {/* Logo — sized big enough to read as a brand mark, not a
          favicon. Keeps to the top of the viewport so the picker
          doesn't fight for attention with it. */}
      <motion.div
        initial={{ opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5 }}
        className="relative z-10 mb-10 sm:mb-14"
      >
        <BrandWordmark height={56} className="opacity-95" />
      </motion.div>

      <div className="relative z-10 flex w-full flex-1 items-center justify-center">
        {selected && selected.has_pin ? (
          <PinPad
            profile={selected}
            pin={pin}
            onPinChange={(next) => {
              setPin(next);
              setPinError(null);
              if (next.length === 4 && !pinAttempting) {
                void commitSwitch(selected, next);
              }
            }}
            onCancel={() => {
              setSelected(null);
              setPin("");
              setPinError(null);
            }}
            isLoading={pinAttempting}
            errorMessage={pinError}
          />
        ) : showWall ? (
          // Two-column split. Picker on the left, poster wall on
          // the right. The grid is `auto / minmax(0, 1fr)` so the
          // wall consumes the slack — picker stays at its natural
          // width, posters fill whatever's left up to a sensible
          // cap (max-w-3xl).
          <div className="grid w-full max-w-7xl items-center gap-12 lg:grid-cols-[auto_minmax(0,1fr)] lg:gap-20">
            {renderPicker("start")}
            <div className="flex justify-center lg:justify-end">
              <PosterWall posters={posters} />
            </div>
          </div>
        ) : (
          // Single column when there isn't enough catalogue art
          // to fill a wall. The aurora carries the canvas alone
          // so the page still feels deliberate, not under-built.
          renderPicker("center")
        )}
      </div>
    </div>
  );
}

// CinematicBackdrop — the layered canvas. Three or four layers
// depending on whether we're rendering the poster wall:
//
//   1. Aurora gradients (always).
//   2. Backdrop blur-mosaic (only when there's NO poster wall —
//      otherwise the wall + mosaic compete and the page reads
//      busy. With a wall, the wall is the "this is your library"
//      signal and the canvas stays calm aurora-only).
//   3. Hover ambient tint — radial bleed in the hovered profile's
//      palette colour. Always on.
//   4. Vertical vignette for card-edge contrast.
function CinematicBackdrop({
  hoveredHex,
  hasWall,
}: {
  hoveredHex: string | null;
  hasWall: boolean;
}) {
  const { data } = useItems(
    { limit: 12, sort_by: "date_added", sort_order: "desc" },
    { staleTime: 5 * 60 * 1000, retry: false },
  );
  const backdrops = useMemo(() => {
    if (hasWall) return []; // wall replaces mosaic as the catalogue signal
    const items = (data?.items ?? []) as MediaItem[];
    return items
      .map((it) => it.backdrop_url)
      .filter((u): u is string => !!u)
      .slice(0, 12);
  }, [data, hasWall]);

  return (
    <>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0"
        style={{
          background: [
            "radial-gradient(60% 70% at 18% 22%, rgba(45, 212, 191, 0.30) 0%, transparent 65%)",
            "radial-gradient(55% 65% at 82% 78%, rgba(13, 148, 136, 0.25) 0%, transparent 60%)",
            "radial-gradient(35% 40% at 50% 95%, rgba(244, 114, 182, 0.08) 0%, transparent 70%)",
          ].join(", "),
        }}
      />
      {backdrops.length > 0 && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 overflow-hidden opacity-[0.07]"
          style={{ filter: "blur(28px) saturate(1.4)" }}
        >
          <div className="grid h-full w-full grid-cols-2 gap-0 sm:grid-cols-3 md:grid-cols-4">
            {backdrops.map((url, i) => (
              <div
                key={i}
                className="h-full w-full bg-cover bg-center"
                style={{ backgroundImage: `url(${url})` }}
              />
            ))}
          </div>
        </div>
      )}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 transition-opacity duration-500"
        style={{
          opacity: hoveredHex ? 1 : 0,
          background: hoveredHex
            ? `radial-gradient(45% 55% at 50% 55%, ${hoveredHex}55 0%, transparent 70%)`
            : "transparent",
        }}
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-gradient-to-b from-bg-base/60 via-transparent to-bg-base/95"
      />
    </>
  );
}

// PosterWall — the right-hand showcase. Six posters in a 3-col
// grid with subtle staggered entry + per-tile float so the wall
// reads as alive without requiring a video loop. Each tile gets
// a tiny rotation (alternating direction) for a casual "spread on
// a table" feel rather than a perfectly grid-aligned mosaic.
function PosterWall({
  posters,
}: {
  posters: { id: string; src: string; title: string }[];
}) {
  return (
    <motion.div
      initial="hidden"
      animate="show"
      variants={{
        hidden: {},
        show: {
          transition: {
            staggerChildren: 0.07,
            delayChildren: 0.3,
          },
        },
      }}
      className="grid w-full max-w-md grid-cols-3 gap-4"
      aria-hidden="true" // decorative — the picker beside it owns the meaning
    >
      {posters.map((p, i) => {
        // Alternate tilt + small vertical offset so the grid feels
        // hand-arranged rather than CSS-perfect. The middle column
        // sits flush; outer columns lean a few degrees.
        const tilt = i % 3 === 0 ? -2 : i % 3 === 2 ? 2 : 0;
        const offsetY = i % 3 === 1 ? -8 : 0;
        return (
          <motion.div
            key={p.id}
            variants={{
              hidden: { opacity: 0, y: 18, rotate: tilt },
              show: { opacity: 1, y: offsetY, rotate: tilt },
            }}
            transition={{ duration: 0.5, ease: [0.22, 0.61, 0.36, 1] }}
            className="aspect-[2/3] overflow-hidden rounded-xl bg-bg-card shadow-2xl ring-1 ring-white/5"
          >
            <img
              src={p.src}
              alt=""
              loading="lazy"
              className="h-full w-full object-cover"
            />
          </motion.div>
        );
      })}
    </motion.div>
  );
}

interface ProfileCardProps {
  profile: ProfileSummary;
  onClick: () => void;
  onHoverChange?: (hovering: boolean) => void;
  /** Compact (list-row) layout when the picker is the side rail
   *  next to the poster wall. Default false → big-tile layout. */
  compact?: boolean;
}

function ProfileCard({
  profile,
  onClick,
  onHoverChange,
  compact = false,
}: ProfileCardProps) {
  const { t } = useTranslation();
  const palette = avatarColorFor(profile.username);
  const initials = getInitials({
    display_name: profile.display_name,
    username: profile.username,
  });

  // Compact (vertical-stack) variant: list row with a smaller avatar
  // on the left, the name on the right, and a subtle background that
  // brightens on hover. Reads as a sidebar list rather than a row of
  // hero tiles, which is what the user asked for ("arriba a abajo")
  // and matches HBO Max / Disney+ TV-shaped surfaces.
  if (compact) {
    return (
      <motion.button
        type="button"
        onClick={onClick}
        onMouseEnter={() => onHoverChange?.(true)}
        onMouseLeave={() => onHoverChange?.(false)}
        onFocus={() => onHoverChange?.(true)}
        onBlur={() => onHoverChange?.(false)}
        variants={{
          hidden: { opacity: 0, x: -16 },
          show: { opacity: 1, x: 0 },
        }}
        transition={{ duration: 0.45, ease: [0.22, 0.61, 0.36, 1] }}
        whileHover={{ x: 4 }}
        whileTap={{ scale: 0.98 }}
        className="group relative flex w-full items-center gap-4 rounded-2xl border border-transparent bg-white/0 px-3 py-2.5 text-left transition-all duration-300 hover:border-white/10 hover:bg-white/5 focus:outline-none focus-visible:border-accent/40"
        aria-label={t("whoIsWatching.pickProfile", {
          name: profile.display_name || profile.username,
        })}
      >
        {/* Halo bleed behind the avatar in the row's palette colour.
            Smaller than the hero variant since the row's height is
            ~ 80 px, but the same recipe so the visual language stays
            consistent across both layouts. */}
        <span
          aria-hidden
          className="absolute left-2 top-1/2 -z-10 h-20 w-20 -translate-y-1/2 rounded-full opacity-0 blur-2xl transition-opacity duration-300 group-hover:opacity-70 group-focus-visible:opacity-70"
          style={{
            background: `radial-gradient(closest-side, ${palette.background}, transparent 70%)`,
          }}
        />
        <div
          className="relative flex h-16 w-16 flex-none items-center justify-center overflow-hidden rounded-full text-2xl font-extralight text-white shadow-lg ring-2 ring-transparent transition-all duration-300 group-hover:ring-white/30 group-focus-visible:ring-accent"
          style={{
            background: `linear-gradient(160deg, ${lighten(palette.background, 0.12)}, ${palette.background} 45%, ${darken(palette.background, 0.18)})`,
          }}
        >
          <span
            aria-hidden
            className="pointer-events-none absolute inset-0 bg-gradient-to-b from-white/10 via-transparent to-transparent"
          />
          <span className="relative">{initials}</span>
          {profile.has_pin && (
            <span
              className="absolute bottom-0 right-0 flex h-5 w-5 items-center justify-center rounded-full bg-black/70 text-white shadow-md backdrop-blur-sm"
              aria-hidden
            >
              <svg
                className="h-2.5 w-2.5"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <rect x="5" y="11" width="14" height="9" rx="2" />
                <path d="M8 11V7a4 4 0 0 1 8 0v4" />
              </svg>
            </span>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-base font-light tracking-wide text-text-primary">
            {profile.display_name || profile.username.split("/").pop()}
          </p>
          {profile.has_pin && (
            <p className="text-xs text-text-muted">
              {t("whoIsWatching.pinRequiredHint", {
                defaultValue: "Pedirá PIN",
              })}
            </p>
          )}
        </div>
      </motion.button>
    );
  }

  // Hero-tile variant — used when the picker is centred (no
  // poster wall). Big circular avatar with name underneath.
  return (
    <motion.button
      type="button"
      onClick={onClick}
      onMouseEnter={() => onHoverChange?.(true)}
      onMouseLeave={() => onHoverChange?.(false)}
      onFocus={() => onHoverChange?.(true)}
      onBlur={() => onHoverChange?.(false)}
      variants={{
        hidden: { opacity: 0, y: 16 },
        show: { opacity: 1, y: 0 },
      }}
      transition={{ duration: 0.45, ease: [0.22, 0.61, 0.36, 1] }}
      whileHover={{ scale: 1.06, y: -4 }}
      whileTap={{ scale: 0.97 }}
      className="group relative flex flex-col items-center gap-4 focus:outline-none"
      aria-label={t("whoIsWatching.pickProfile", {
        name: profile.display_name || profile.username,
      })}
    >
      <span
        aria-hidden
        className="absolute -inset-6 rounded-full opacity-0 blur-2xl transition-opacity duration-300 group-hover:opacity-70 group-focus-visible:opacity-70"
        style={{
          background: `radial-gradient(closest-side, ${palette.background}, transparent 70%)`,
        }}
      />

      <div
        className="relative flex h-36 w-36 items-center justify-center overflow-hidden rounded-full text-5xl font-extralight tracking-tight text-white shadow-2xl ring-2 ring-transparent transition-all duration-300 group-hover:ring-white/30 group-focus-visible:ring-accent group-focus-visible:ring-offset-4 group-focus-visible:ring-offset-transparent sm:h-40 sm:w-40 sm:text-6xl"
        style={{
          background: `linear-gradient(160deg, ${lighten(palette.background, 0.12)}, ${palette.background} 45%, ${darken(palette.background, 0.18)})`,
        }}
      >
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 bg-gradient-to-b from-white/10 via-transparent to-transparent"
        />
        <span className="relative">{initials}</span>
        {profile.has_pin && (
          <span
            className="absolute bottom-2.5 right-2.5 flex h-7 w-7 items-center justify-center rounded-full bg-black/70 text-white shadow-md backdrop-blur-sm"
            aria-hidden
          >
            <svg
              className="h-3.5 w-3.5"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
            >
              <rect x="5" y="11" width="14" height="9" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 8 0v4" />
            </svg>
          </span>
        )}
      </div>
      <span className="relative max-w-[10rem] truncate text-base font-light tracking-wide text-text-secondary transition-colors group-hover:text-text-primary">
        {profile.display_name || profile.username.split("/").pop()}
      </span>
    </motion.button>
  );
}

interface PinPadProps {
  profile: ProfileSummary;
  pin: string;
  onPinChange: (v: string) => void;
  onCancel: () => void;
  isLoading: boolean;
  errorMessage: string | null;
}

function PinPad({
  profile,
  pin,
  onPinChange,
  onCancel,
  isLoading,
  errorMessage,
}: PinPadProps) {
  const { t } = useTranslation();
  const palette = avatarColorFor(profile.username);
  const initials = getInitials({
    display_name: profile.display_name,
    username: profile.username,
  });
  const inputRef = useRef<HTMLInputElement>(null);

  const focusInput = () => inputRef.current?.focus();

  useEffect(() => {
    if (errorMessage) {
      focusInput();
    }
  }, [errorMessage]);

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3 }}
      className="flex w-full max-w-sm flex-col items-center gap-6"
    >
      <motion.div
        className="relative flex h-28 w-28 items-center justify-center overflow-hidden rounded-full text-3xl font-extralight text-white shadow-2xl"
        style={{
          background: `linear-gradient(160deg, ${lighten(palette.background, 0.12)}, ${palette.background} 45%, ${darken(palette.background, 0.18)})`,
        }}
        animate={errorMessage ? { x: [0, -6, 6, -4, 4, 0] } : {}}
        transition={{ duration: 0.4 }}
      >
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 bg-gradient-to-b from-white/10 via-transparent to-transparent"
        />
        {initials}
      </motion.div>

      <div className="flex flex-col items-center gap-1">
        <h2 className="text-2xl font-light tracking-wide text-text-primary">
          {profile.display_name || profile.username.split("/").pop()}
        </h2>
        <p className="text-sm text-text-muted">{t("whoIsWatching.enterPin")}</p>
      </div>

      <div
        className="relative flex flex-col items-center gap-3"
        onClick={focusInput}
      >
        <div className="flex gap-3">
          {[0, 1, 2, 3].map((i) => {
            const filled = i < pin.length;
            const active = i === pin.length && !isLoading;
            return (
              <div
                key={i}
                className={[
                  "flex h-14 w-12 items-center justify-center rounded-lg border-2 transition-all",
                  filled
                    ? "border-accent bg-accent/5"
                    : active
                      ? "border-accent/60 ring-4 ring-accent/15"
                      : "border-border bg-bg-card/40",
                ].join(" ")}
              >
                {filled && (
                  <motion.span
                    initial={{ scale: 0 }}
                    animate={{ scale: 1 }}
                    transition={{ type: "spring", stiffness: 500, damping: 25 }}
                    className="block h-3 w-3 rounded-full bg-text-primary"
                  />
                )}
              </div>
            );
          })}
        </div>

        <input
          ref={inputRef}
          type="tel"
          inputMode="numeric"
          pattern="[0-9]*"
          autoFocus
          autoComplete="off"
          maxLength={4}
          value={pin}
          onChange={(e) =>
            onPinChange(e.target.value.replace(/[^0-9]/g, "").slice(0, 4))
          }
          aria-label={t("whoIsWatching.pinInputLabel")}
          className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
          disabled={isLoading}
        />
      </div>

      <div className="h-5">
        {errorMessage && (
          <motion.p
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="text-center text-sm text-error"
          >
            {errorMessage}
          </motion.p>
        )}
        {isLoading && !errorMessage && (
          <p className="text-center text-sm text-text-muted">
            {t("whoIsWatching.unlocking", { defaultValue: "Desbloqueando…" })}
          </p>
        )}
      </div>

      <button
        type="button"
        onClick={onCancel}
        disabled={isLoading}
        className="text-sm text-text-muted transition-colors hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-50"
      >
        {t("common.cancel")}
      </button>
    </motion.div>
  );
}

// Same colour helpers as before — stays inline to keep the file
// self-contained. A 12 %/18 % linear shift on each end of the
// avatar's base colour is the cheapest "object lit from above"
// effect that doesn't break colour identity.
function lighten(hex: string, amount: number): string {
  return shift(hex, amount);
}
function darken(hex: string, amount: number): string {
  return shift(hex, -amount);
}
function shift(hex: string, amount: number): string {
  const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  if (!m) return hex;
  const r = clamp(parseInt(m[1], 16) + Math.round(255 * amount));
  const g = clamp(parseInt(m[2], 16) + Math.round(255 * amount));
  const b = clamp(parseInt(m[3], 16) + Math.round(255 * amount));
  return `rgb(${r}, ${g}, ${b})`;
}
function clamp(n: number): number {
  return Math.max(0, Math.min(255, n));
}
