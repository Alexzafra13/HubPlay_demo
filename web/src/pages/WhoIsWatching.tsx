// WhoIsWatching — cinematic profile picker. Shown after login (and
// via TopBar "Cambiar perfil") whenever the current account has
// more than one profile in the tree (parent + ≥1 child). Solo
// accounts skip this screen entirely and go straight to /.
//
// Visual brief (post-2026-05 redesign — see git for the
// "premium feel" review notes):
//
//   - The canvas works in TWO modes that should both feel premium:
//
//       * "with content": pull a handful of recent backdrops from
//         the catalogue and tile them in a heavily-blurred mosaic
//         at very low opacity. Reads as "this is YOUR library"
//         without ever being a recognisable poster.
//
//       * "fresh install": the same aurora gradients the Login
//         page uses, so the auth canvas is one continuous moment.
//
//     Either way, an ambient tint follows the hovered profile —
//     a radial gradient that picks up that profile's avatar
//     palette colour. The room "lights up" where the cursor is.
//
//   - Cards are ~160 px (vs the previous 128) with a soft inner
//     light gradient on top + a deeper colour on the bottom so
//     the tile reads as an object, not a flat swatch. Hover
//     scales 1.06, lifts -4 px, and adds a halo of that profile's
//     palette colour at 20 % alpha behind the card.
//
//   - Typography goes light + tracked. Title shifts to font-weight
//     200 with letter-spacing; subtitle softer.
//
//   - PIN entry stays as the four-box pad we shipped before — it
//     was already the right pattern.
//
// PIN-protected profiles surface the dot-boxes on click; wrong PIN
// intentionally maps to the same generic-error path the wrong-
// password login does so PIN guessing can't be distinguished from
// an inactive profile.

import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";
import { LogOut, Pencil } from "lucide-react";
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

  // Hovered profile drives the ambient room-tint behind the grid
  // (radial gradient that picks up that profile's palette colour).
  // Stored at the page level so the backdrop layer can react.
  const [hoveredProfileId, setHoveredProfileId] = useState<string | null>(null);

  // Solo account → bounce home. Login routes everyone through this
  // page so the picker is the single source of truth on "do I
  // actually need to choose?" — when the answer is no, we silently
  // navigate away.
  useEffect(() => {
    if (!isLoading && !error && profiles && profiles.length <= 1) {
      navigate("/", { replace: true });
    }
  }, [isLoading, error, profiles, navigate]);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-bg-base">
        <Spinner size="lg" />
      </div>
    );
  }

  // Render the friendly fallback page on a real failure so the
  // operator never stares at an empty grid wondering what broke.
  const profilesMissing = !profiles || profiles.length === 0;
  if (error || profilesMissing) {
    return (
      <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-bg-base px-6 py-12">
        <CinematicBackdrop hoveredHex={null} />
        <div className="relative z-10 flex max-w-md flex-col items-center gap-5 text-center">
          <BrandWordmark height={28} className="opacity-80" />
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
              className="rounded-md border border-border-subtle bg-bg-card/50 px-3 py-1.5 text-xs text-text-muted backdrop-blur-sm transition-colors hover:bg-bg-card hover:text-text-primary"
            >
              {t("whoIsWatching.continueAnyway", {
                defaultValue: "Continuar al inicio",
              })}
            </button>
            <button
              type="button"
              onClick={() => void handleSignOut()}
              className="inline-flex items-center gap-1.5 rounded-md border border-border-subtle bg-bg-card/50 px-3 py-1.5 text-xs text-text-muted backdrop-blur-sm transition-colors hover:bg-bg-card hover:text-text-primary"
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
      // Same-profile switch is a no-op — picking the parent (which
      // is the JWT identity here) yields the same token. Skip the
      // round-trip and just send them home.
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

  // Only admins see the "manage profiles" shortcut — non-admin
  // members of a family account can't open the admin pane.
  const canManage = me?.role === "admin";

  // Hovered hex powers the ambient room-tint behind the picker.
  // Computed once per hover change rather than re-derived inside
  // CinematicBackdrop so the gradient string stays stable.
  const hoveredHex = useMemo(() => {
    if (!hoveredProfileId) return null;
    const p = profiles?.find((x) => x.id === hoveredProfileId);
    return p ? avatarColorFor(p.username).background : null;
  }, [hoveredProfileId, profiles]);

  return (
    <div
      className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-bg-base px-6 py-12"
      onMouseLeave={() => setHoveredProfileId(null)}
    >
      <CinematicBackdrop hoveredHex={hoveredHex} />

      <div className="relative z-10 flex w-full flex-col items-center">
        <BrandWordmark height={28} className="mb-12 opacity-80" />

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
        ) : (
          <>
            <motion.h1
              initial={{ opacity: 0, y: -6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.4, ease: [0.22, 0.61, 0.36, 1] }}
              className="mb-3 text-center text-5xl font-extralight tracking-[-0.01em] text-text-primary sm:text-6xl"
              style={{ letterSpacing: "-0.015em" }}
            >
              {t("whoIsWatching.title")}
            </motion.h1>
            <motion.p
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.5, delay: 0.15 }}
              className="mb-14 text-sm text-text-muted tracking-wider"
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
              className="flex flex-wrap items-start justify-center gap-8 sm:gap-12"
            >
              {profiles?.map((p) => (
                <ProfileCard
                  key={p.id}
                  profile={p}
                  onClick={() => void pickProfile(p)}
                  onHoverChange={(h) =>
                    setHoveredProfileId(h ? p.id : null)
                  }
                />
              ))}
            </motion.div>
          </>
        )}
      </div>

      {/* Bottom rail — glassier, with a cleaner separation between
          "manage" (admin) and "sign out" (any user). Always visible
          when not in PIN mode so the operator never feels trapped. */}
      {!selected && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.5, delay: 0.5 }}
          className="relative z-10 mt-16 flex flex-wrap items-center justify-center gap-3 text-xs"
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
            className="inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/5 px-4 py-2 text-text-secondary backdrop-blur-md transition-all hover:border-white/20 hover:bg-white/10 hover:text-text-primary"
          >
            <LogOut className="h-3.5 w-3.5" />
            {t("whoIsWatching.signOut", {
              defaultValue: "Cerrar sesión",
            })}
          </button>
        </motion.div>
      )}
    </div>
  );
}

// CinematicBackdrop — the layered canvas that sits behind the picker.
// Three stacked layers, painter's-algorithm bottom-up:
//
//   1. Aurora gradients (always, even when no content) — same recipe
//      as the Login page so the auth surface is one continuous look.
//   2. Backdrop mosaic — when there's at least one item with a
//      backdrop, tile the most-recent ones at heavy blur + 6 %
//      opacity. Falls through silently when no content (fresh
//      install, new account, etc.) so this is purely additive
//      polish and never breaks the page.
//   3. Hover ambient tint — when the user is hovering a profile,
//      a soft radial gradient in that profile's palette colour
//      bleeds in behind the grid. The room "lights up" where the
//      cursor is. Smooth opacity transition so flicking between
//      cards reads as a glow that follows.
//   4. Vertical vignette to keep card edges legible.
function CinematicBackdrop({ hoveredHex }: { hoveredHex: string | null }) {
  // Pull a small slice of recent items with backdrops. Capped at 12
  // so the mosaic stays a 4×3 grid even on ultrawide. We don't care
  // if the request fails — the aurora alone is the fallback.
  // Sorted by date_added DESC so the canvas evolves as the catalogue
  // grows ("the picker remembers what you just imported").
  const { data } = useItems(
    {
      limit: 12,
      sort_by: "date_added",
      sort_order: "desc",
    },
    {
      // Picker isn't user-data; backdrops change rarely. Generous
      // staleTime saves a refetch on every navigation back here.
      staleTime: 5 * 60 * 1000,
      // Failures are non-fatal (no content yet, parent JWT can't
      // hit /items, etc.). Don't retry — slows the picker for no
      // gain.
      retry: false,
    },
  );
  const backdrops = useMemo(() => {
    const items = (data?.items ?? []) as MediaItem[];
    return items
      .map((it) => it.backdrop_url)
      .filter((u): u is string => !!u)
      .slice(0, 12);
  }, [data]);

  return (
    <>
      {/* Layer 1 — aurora. Always on. */}
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

      {/* Layer 2 — backdrop mosaic. Renders only when we actually
          have catalogue art. Heavy blur + low opacity so individual
          posters are unrecognisable; the eye reads "warm room
          lit by the room's own contents". */}
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

      {/* Layer 3 — hover ambient. Radial wash in the hovered
          profile's palette colour. Opacity transition so a flick
          across cards reads as a single travelling glow. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 transition-opacity duration-500"
        style={{
          opacity: hoveredHex ? 1 : 0,
          background: hoveredHex
            ? `radial-gradient(45% 55% at 50% 55%, ${hoveredHex}66 0%, transparent 70%)`
            : "transparent",
        }}
      />

      {/* Layer 4 — vignette so the card edges + bottom rail keep
          contrast against the mosaic. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-gradient-to-b from-bg-base/80 via-transparent to-bg-base/95"
      />
    </>
  );
}

interface ProfileCardProps {
  profile: ProfileSummary;
  onClick: () => void;
  onHoverChange?: (hovering: boolean) => void;
}

function ProfileCard({ profile, onClick, onHoverChange }: ProfileCardProps) {
  const { t } = useTranslation();
  const palette = avatarColorFor(profile.username);
  const initials = getInitials({
    display_name: profile.display_name,
    username: profile.username,
  });
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
      {/* Halo behind the card. Same palette colour, blurred and
          scaled up so it reads as ambient light on the wall behind
          the tile rather than a flat outline. */}
      <span
        aria-hidden
        className="absolute -inset-6 rounded-3xl opacity-0 blur-2xl transition-opacity duration-300 group-hover:opacity-70 group-focus-visible:opacity-70"
        style={{
          background: `radial-gradient(closest-side, ${palette.background}, transparent 75%)`,
        }}
      />

      {/* The tile itself. The inner gradient on top + darker on the
          bottom is the "this is an object" trick — a flat colour
          would read like a swatch. The white/10 ring picks up on
          hover and matches Netflix's "selected" affordance. */}
      <div
        className="relative flex h-40 w-40 items-center justify-center overflow-hidden rounded-2xl text-5xl font-extralight tracking-tight text-white shadow-2xl ring-2 ring-transparent transition-all duration-300 group-hover:ring-white/30 group-focus-visible:ring-accent group-focus-visible:ring-offset-4 group-focus-visible:ring-offset-transparent sm:h-44 sm:w-44 sm:text-6xl"
        style={{
          background: `linear-gradient(160deg, ${lighten(palette.background, 0.12)}, ${palette.background} 45%, ${darken(palette.background, 0.18)})`,
        }}
      >
        {/* Inner top-light gloss for depth. */}
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

  // After a wrong PIN we clear the field; refocus immediately so
  // the operator can retype without an extra tap.
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
        className="flex h-28 w-28 items-center justify-center overflow-hidden rounded-2xl text-3xl font-extralight text-white shadow-2xl"
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

// Small colour helpers for the tile depth gradient. We don't pull
// in a colour library — the avatar palette is hand-picked dark
// tones, and a 12 %/18 % linear shift on each end is enough for
// the highlight + shadow effect without breaking colour identity.
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
