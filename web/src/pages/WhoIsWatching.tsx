// WhoIsWatching — Netflix-style profile picker. Shown after login
// (and via TopBar "Cambiar perfil") whenever the current account
// has more than one profile in the tree (parent + ≥1 child). Solo
// accounts skip this screen entirely and go straight to /.
//
// Visual brief:
//   - Aurora backdrop matching the Login page so the canvas feels
//     part of the same product family rather than a bare auth shell.
//   - Avatar cards animate in with framer-motion stagger so the grid
//     feels alive on first paint.
//   - PIN entry is rendered as four discrete dot-boxes (one per
//     digit) backed by a hidden input that captures real keyboard
//     events. Auto-submits once the fourth digit lands so the user
//     never has to reach for "Desbloquear" — Netflix / Disney+
//     parity, less friction.
//   - Sign-out and (admin-only) "Gestionar perfiles" sit on the
//     bottom rail so an admin who clicked into the wrong account
//     isn't trapped.
//
// PIN-protected profiles surface the dot-boxes on click; wrong PIN
// intentionally maps to the same generic-error path the wrong-
// password login does so PIN guessing can't be distinguished from
// an inactive profile.

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";
import { LogOut, Pencil } from "lucide-react";
import { useLogout, useMe, useProfiles, useSwitchProfile } from "@/api/hooks";
import type { ProfileSummary } from "@/api/types";
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

  // Solo account → don't even render the picker; bounce home. The
  // login flow will skip this screen too in the happy path; this
  // branch covers users who land directly on /select-profile.
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
      // Same-profile switch is a no-op — the picker's identity is
      // technically the parent's user_id when it lands here on
      // first login, so picking the parent yields the same JWT.
      // Skip the round-trip and just send them home.
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
  // members of a family account can't open the admin pane anyway,
  // so the link would 403 them.
  const canManage = me?.role === "admin";

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-bg-base px-6 py-12">
      <AuroraBackdrop />

      <div className="relative z-10 flex w-full flex-col items-center">
        <BrandWordmark height={28} className="mb-12 opacity-80" />

        {selected && selected.has_pin ? (
          <PinPad
            profile={selected}
            pin={pin}
            onPinChange={(next) => {
              setPin(next);
              setPinError(null);
              // Netflix-style auto-submit: when the fourth digit
              // lands, fire the switch immediately. The user never
              // has to reach for an Enter / Unlock affordance.
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
              transition={{ duration: 0.35, ease: "easeOut" }}
              className="mb-3 text-4xl font-light tracking-tight text-text-primary sm:text-5xl"
            >
              {t("whoIsWatching.title")}
            </motion.h1>
            <motion.p
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.4, delay: 0.1 }}
              className="mb-12 text-sm text-text-muted"
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
                    staggerChildren: 0.06,
                    delayChildren: 0.15,
                  },
                },
              }}
              className="flex flex-wrap items-start justify-center gap-6 sm:gap-8"
            >
              {profiles?.map((p) => (
                <ProfileCard
                  key={p.id}
                  profile={p}
                  onClick={() => void pickProfile(p)}
                />
              ))}
            </motion.div>
          </>
        )}
      </div>

      {/* Bottom rail. Subtle so it doesn't compete with the picker
          itself — the user's attention belongs on the avatars. */}
      {!selected && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.4, delay: 0.4 }}
          className="relative z-10 mt-12 flex flex-wrap items-center justify-center gap-2 text-xs"
        >
          {canManage && (
            <button
              type="button"
              onClick={() => navigate("/admin/users")}
              className="inline-flex items-center gap-1.5 rounded-md border border-border-subtle bg-bg-card/50 px-3 py-1.5 text-text-muted backdrop-blur-sm transition-colors hover:bg-bg-card hover:text-text-primary"
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
            className="inline-flex items-center gap-1.5 rounded-md border border-border-subtle bg-bg-card/50 px-3 py-1.5 text-text-muted backdrop-blur-sm transition-colors hover:bg-bg-card hover:text-text-primary"
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

// AuroraBackdrop — same vocabulary as Login.tsx so the auth canvas
// reads as one continuous moment. Three radial gradients stacked
// (vibrant teal upper-left, deeper teal lower-right, warm halo
// center-bottom) plus a vignette for card legibility. Pure CSS,
// pointer-events disabled so it never intercepts clicks.
function AuroraBackdrop() {
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
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-gradient-to-b from-transparent via-bg-base/40 to-bg-base/80"
      />
    </>
  );
}

interface ProfileCardProps {
  profile: ProfileSummary;
  onClick: () => void;
}

function ProfileCard({ profile, onClick }: ProfileCardProps) {
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
      variants={{
        hidden: { opacity: 0, y: 12 },
        show: { opacity: 1, y: 0 },
      }}
      transition={{ duration: 0.35, ease: "easeOut" }}
      whileHover={{ scale: 1.06 }}
      whileTap={{ scale: 0.97 }}
      className="group flex flex-col items-center gap-3 focus:outline-none"
      aria-label={t("whoIsWatching.pickProfile", {
        name: profile.display_name || profile.username,
      })}
    >
      <div
        className="relative flex h-32 w-32 items-center justify-center rounded-2xl text-3xl font-semibold text-white shadow-xl ring-2 ring-transparent transition-all group-hover:ring-white/20 group-focus-visible:ring-accent group-focus-visible:ring-offset-4 group-focus-visible:ring-offset-bg-base sm:h-36 sm:w-36 sm:text-4xl"
        style={{ background: palette.background }}
      >
        {initials}
        {profile.has_pin && (
          <span
            className="absolute bottom-2 right-2 flex h-7 w-7 items-center justify-center rounded-full bg-black/70 text-white shadow-md backdrop-blur-sm"
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
      <span className="max-w-[10rem] truncate text-sm font-medium text-text-secondary transition-colors group-hover:text-text-primary">
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

// PinPad — Netflix-style 4-box PIN entry. The boxes are presentation
// only; a single hidden <input type=tel> captures actual keystrokes
// (including iOS / Android numeric keyboards). Clicking any box
// re-focuses the input so the keyboard never disappears mid-entry.
//
// Visual states per box:
//   - empty:  faint outline
//   - filled: outline becomes accent + a centered solid dot
//   - active (next to fill): outline pulses with accent ring
//
// Auto-submit fires from the parent when pin.length hits 4, so we
// don't need an explicit "Unlock" button — Cancel stays as the
// "wrong avatar" exit.
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

  // Re-focus the hidden input whenever the boxes are tapped so the
  // soft keyboard reopens on mobile after an error shake.
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
        className="flex h-24 w-24 items-center justify-center rounded-2xl text-2xl font-semibold text-white shadow-xl"
        style={{ background: palette.background }}
        animate={errorMessage ? { x: [0, -6, 6, -4, 4, 0] } : {}}
        transition={{ duration: 0.4 }}
      >
        {initials}
      </motion.div>

      <div className="flex flex-col items-center gap-1">
        <h2 className="text-2xl font-semibold text-text-primary">
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

        {/* Hidden input. opacity-0 + pointer-events-none on the box
            wrapper keeps the visual boxes as the click target. We
            keep it inside the layout (not absolute) with width 1px
            so screen readers + iOS still treat it as a real form
            field for focus and keyboard delivery. */}
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
