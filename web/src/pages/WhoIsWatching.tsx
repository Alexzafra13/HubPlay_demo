// WhoIsWatching — Netflix-style profile picker. Shown after login
// (and via TopBar "Cambiar perfil") whenever the current account
// has more than one profile in the tree (parent + ≥1 child). Solo
// accounts skip this screen entirely and go straight to /.
//
// PIN-protected profiles surface a 4-digit input on click; wrong
// PIN intentionally maps to the same generic-error path the wrong-
// password login does so PIN guessing can't be distinguished from
// an inactive profile.

import { useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useProfiles, useSwitchProfile } from "@/api/hooks";
import type { ProfileSummary } from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Button, Spinner } from "@/components/common";
import { avatarColorFor } from "@/utils/avatarColor";
import { getInitials } from "@/utils/userDisplay";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

export default function WhoIsWatching() {
  const { t } = useTranslation();
  const { data: profiles, isLoading, error } = useProfiles();
  const { user, refreshMe } = useAuthStore();
  const navigate = useNavigate();
  const switchProfile = useSwitchProfile();

  const [selected, setSelected] = useState<ProfileSummary | null>(null);
  const [pin, setPin] = useState("");
  const [pinError, setPinError] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-bg-base">
        <Spinner size="lg" />
      </div>
    );
  }

  // Solo account → don't even render the picker; bounce home. The
  // login flow will skip this screen too in the happy path; this
  // branch covers users who land directly on /select-profile.
  if (!error && profiles && profiles.length <= 1) {
    navigate("/", { replace: true });
    return null;
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
    try {
      // Same-profile switch is a no-op — the profile picker is
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
    }
  }

  function onSubmitPin(e: FormEvent) {
    e.preventDefault();
    if (!selected) return;
    if (pin.length !== 4) {
      setPinError(t("whoIsWatching.errorPinLength"));
      return;
    }
    void commitSwitch(selected, pin);
  }

  return (
    <div className="min-h-screen bg-bg-base flex flex-col items-center justify-center px-6 py-12">
      <BrandWordmark height={28} className="mb-12 opacity-80" />

      {selected && selected.has_pin ? (
        <PinPad
          profile={selected}
          pin={pin}
          onPinChange={setPin}
          onSubmit={onSubmitPin}
          onCancel={() => {
            setSelected(null);
            setPin("");
            setPinError(null);
          }}
          isLoading={switchProfile.isPending}
          errorMessage={pinError}
        />
      ) : (
        <>
          <h1 className="mb-12 text-4xl font-light text-text-primary tracking-tight">
            {t("whoIsWatching.title")}
          </h1>
          <div className="flex flex-wrap items-start justify-center gap-8">
            {profiles?.map((p) => (
              <ProfileCard
                key={p.id}
                profile={p}
                onClick={() => void pickProfile(p)}
              />
            ))}
          </div>
        </>
      )}
    </div>
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
    <button
      type="button"
      onClick={onClick}
      className="flex flex-col items-center gap-3 group focus:outline-none"
      aria-label={t("whoIsWatching.pickProfile", {
        name: profile.display_name || profile.username,
      })}
    >
      <div
        className="relative flex h-32 w-32 items-center justify-center rounded-2xl text-3xl font-semibold text-white shadow-lg transition-transform group-hover:scale-105 group-focus-visible:ring-2 group-focus-visible:ring-accent group-focus-visible:ring-offset-4 group-focus-visible:ring-offset-bg-base"
        style={{ background: palette.background }}
      >
        {initials}
        {profile.has_pin && (
          <span
            className="absolute bottom-1.5 right-1.5 flex h-7 w-7 items-center justify-center rounded-full bg-black/70 text-white shadow-md backdrop-blur-sm"
            aria-hidden
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <rect x="5" y="11" width="14" height="9" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 8 0v4" />
            </svg>
          </span>
        )}
      </div>
      <span className="max-w-[10rem] truncate text-sm font-medium text-text-secondary group-hover:text-text-primary transition-colors">
        {profile.display_name || profile.username.split("/").pop()}
      </span>
    </button>
  );
}

interface PinPadProps {
  profile: ProfileSummary;
  pin: string;
  onPinChange: (v: string) => void;
  onSubmit: (e: FormEvent) => void;
  onCancel: () => void;
  isLoading: boolean;
  errorMessage: string | null;
}

function PinPad({
  profile,
  pin,
  onPinChange,
  onSubmit,
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
  return (
    <form onSubmit={onSubmit} className="flex flex-col items-center gap-6 w-full max-w-xs">
      <div
        className="flex h-24 w-24 items-center justify-center rounded-2xl text-2xl font-semibold text-white shadow-lg"
        style={{ background: palette.background }}
      >
        {initials}
      </div>
      <h2 className="text-2xl font-semibold text-text-primary text-center">
        {profile.display_name || profile.username.split("/").pop()}
      </h2>
      <p className="text-sm text-text-muted text-center">
        {t("whoIsWatching.enterPin")}
      </p>
      <input
        type="tel"
        inputMode="numeric"
        pattern="[0-9]*"
        autoFocus
        autoComplete="off"
        maxLength={4}
        value={pin}
        onChange={(e) => onPinChange(e.target.value.replace(/[^0-9]/g, "").slice(0, 4))}
        className="w-40 rounded-lg border border-border bg-bg-card px-4 py-3 text-center text-2xl font-mono tracking-[0.6em] text-text-primary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
        aria-label={t("whoIsWatching.pinInputLabel")}
      />
      {errorMessage && (
        <p className="text-sm text-error text-center">{errorMessage}</p>
      )}
      <div className="flex gap-3 w-full">
        <Button
          type="button"
          variant="secondary"
          onClick={onCancel}
          className="flex-1"
        >
          {t("common.cancel")}
        </Button>
        <Button
          type="submit"
          isLoading={isLoading}
          disabled={pin.length !== 4}
          className="flex-1"
        >
          {t("whoIsWatching.unlock")}
        </Button>
      </div>
    </form>
  );
}
