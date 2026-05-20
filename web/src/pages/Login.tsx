import { useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { m } from "framer-motion";
import { useLogin } from "@/api/hooks";
import { ApiError } from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Button } from "@/components/common";
import { Input } from "@/components/common";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

// loginErrorMessage maps an unauthenticated error to operator-
// friendly copy. The wire-level messages from the server ("account
// is disabled", "temporary access window has expired") are accurate
// but English and feel like debug strings; we translate them here
// AND tailor each one to the action the user has to take next.
function loginErrorMessage(
  err: Error,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case "ACCESS_EXPIRED":
        return t("login.errorAccessExpired", {
          defaultValue:
            "Tu acceso temporal ha caducado. Contacta con el administrador del servidor para extenderlo.",
        });
      case "ACCOUNT_DISABLED":
        return t("login.errorAccountDisabled", {
          defaultValue:
            "Tu cuenta está desactivada. Contacta con el administrador del servidor.",
        });
      case "INVALID_CREDENTIALS":
        return t("login.errorInvalidCredentials", {
          defaultValue: "Usuario o contraseña incorrectos.",
        });
      case "RATE_LIMITED":
      case "TOO_MANY_REQUESTS":
        return t("login.errorRateLimited", {
          defaultValue:
            "Demasiados intentos. Espera unos minutos e inténtalo de nuevo.",
        });
    }
  }
  return err.message || t("login.loginFailed");
}

// Login — visual rework 2026-05. The form itself wasn't the
// problem (the user explicitly liked it) but the canvas around
// it read like "any admin login". Now it follows the same
// vocabulary the WhoIsWatching picker uses post-cinematic-pass:
//
//   - Big BrandWordmark above the card so the brand owns the
//     top of the viewport instead of hiding inside an 8 px box.
//   - A "Bienvenido" hero with the same extralight + tracked
//     type the picker uses, so the auth flow reads as one
//     continuous moment.
//   - Aurora backdrop plus a set of ghosted "poster silhouettes"
//     drifting slowly behind the card. They're not real
//     content (the user is unauthenticated so we can't fetch
//     /items here) — they're rounded rects coloured with the
//     avatar palette + a slow infinite drift, just enough to
//     suggest "media platform" without being literal.
//   - Form card unchanged in structure, slightly more breathing
//     room and the redundant inline logo dropped (the big one
//     above is the brand mark now).
export default function Login() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const login = useLogin();
  const setAuth = useAuthStore((s) => s.setAuth);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    login.mutate(
      { username, password },
      {
        onSuccess(data) {
          setAuth(data.user);
          if (data.user.password_change_required) {
            navigate("/change-password");
          } else if ((data.profiles?.length ?? 0) > 1) {
            navigate("/select-profile");
          } else {
            navigate("/");
          }
        },
        onError(err) {
          setError(loginErrorMessage(err, t));
        },
      },
    );
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-bg-base px-4 py-12">
      {/* Layer 1 — aurora. Three stacked radial gradients for the
          warm/cool wash the picker also uses, so the auth shell
          reads as a single visual family. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0"
        style={{
          background: [
            "radial-gradient(60% 70% at 18% 22%, rgba(45, 212, 191, 0.35) 0%, transparent 65%)",
            "radial-gradient(55% 65% at 82% 78%, rgba(13, 148, 136, 0.28) 0%, transparent 60%)",
            "radial-gradient(35% 40% at 50% 95%, rgba(244, 114, 182, 0.10) 0%, transparent 70%)",
          ].join(", "),
        }}
      />

      {/* Layer 2 — drifting ghosted poster silhouettes. Decorative
          scaffolding that suggests "this is a media app" without
          requiring catalogue data we can't fetch from a logged-out
          session. Keep them outside the card by virtue of fixed
          positioning + low z, and at small max-w on narrow viewports
          so they don't crowd the form. */}
      <GhostPosters />

      {/* Layer 3 — vignette so the card edges read against the
          glow. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-gradient-to-b from-transparent via-bg-base/40 to-bg-base/80"
      />

      {/* Foreground card. Logo lives back INSIDE the card (per user
          feedback — pulling it out broke the form's visual unity)
          and there's no surrounding hero copy: the brand mark
          alone carries the page identity, and the GhostPosters
          drifting behind do the cinematic lifting. */}
      <m.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{
          duration: 0.5,
          ease: [0.22, 0.61, 0.36, 1],
        }}
        className="relative z-10 w-full max-w-sm rounded-2xl border border-white/10 bg-bg-card/70 p-8 shadow-2xl backdrop-blur-xl"
      >
        <div className="mb-7 flex justify-center">
          <BrandWordmark height={48} />
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Input
            label={t("login.username")}
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder={t("login.usernamePlaceholder")}
            autoComplete="username"
            required
          />

          <Input
            label={t("login.password")}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t("login.passwordPlaceholder")}
            autoComplete="current-password"
            required
          />

          {error && (
            <p
              role="alert"
              className="rounded-md border border-error/30 bg-error/10 px-3 py-2 text-sm text-error"
            >
              {error}
            </p>
          )}

          <Button
            type="submit"
            fullWidth
            size="lg"
            isLoading={login.isPending}
            className="mt-2"
          >
            {t("login.signIn")}
          </Button>
        </form>

        {/* Pairing entry. TVs / consoles where typing a password on a
            remote is awful land on /pair instead. The link is small
            so it does not compete with the primary login flow. */}
        <p className="mt-5 text-center text-xs text-text-muted">
          {t("login.pairPrompt", {
            defaultValue: "¿Estás en una TV o consola? ",
          })}
          <a
            href="/pair"
            className="font-medium text-accent hover:underline"
          >
            {t("login.pairCta", { defaultValue: "Vincula este dispositivo" })}
          </a>
        </p>
      </m.div>
    </div>
  );
}

// GhostPosters — six rounded-rectangle silhouettes positioned around
// the form card and drifting slowly via framer-motion infinite
// animations. They're aria-hidden decorative scaffolding only —
// the auth flow needs no catalogue context, but a flat aurora
// + card reads as "yet another admin login". Adding a few
// suggestive shapes at low opacity is the cheapest way to plant
// "this is a media app" in the user's eye without resorting to a
// hardcoded image asset that ages.
//
// Sizes + positions are hand-chosen to look composed (a few near,
// a few far, one offset) rather than evenly spaced. Colour comes
// from a subset of the avatar palette so the same chromatic
// vocabulary covers every authenticated surface too.
function GhostPosters() {
  // Hand-picked frames. Position is in % of viewport so it
  // scales; size is in fractional viewport units so it adapts
  // without overwhelming on small screens. Each entry's `delay`
  // staggers the float animation so the wall doesn't pulse in
  // unison. Tilts (rotate) keep the grid from looking
  // CSS-perfect.
  const ghosts = [
    { top: "8%",  left: "6%",   w: "8rem",  h: "12rem", color: "#3d5a40", rotate: -7,  delay: 0 },
    { top: "14%", left: "78%",  w: "9rem",  h: "13rem", color: "#1e3252", rotate: 5,   delay: 0.4 },
    { top: "55%", left: "10%",  w: "10rem", h: "15rem", color: "#5c3d6e", rotate: 4,   delay: 0.8 },
    { top: "62%", left: "82%",  w: "8.5rem", h: "12.5rem", color: "#7a3d2e", rotate: -6, delay: 1.2 },
    { top: "75%", left: "40%",  w: "7rem",  h: "10.5rem", color: "#2e5c5a", rotate: -3, delay: 0.6 },
    { top: "26%", left: "44%",  w: "7.5rem", h: "11rem", color: "#5a3d3d", rotate: 6,  delay: 1.4 },
  ];

  return (
    <div
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 hidden md:block"
    >
      {ghosts.map((g, i) => (
        // Cada ghost tiene posición top+left única (lo garantiza el
        // generador del array, no son posibles colisiones). El índice
        // se usa sólo para escalonar la duración de la animación.
        <m.div
          key={`${g.top}-${g.left}`}
          className="absolute rounded-2xl shadow-2xl"
          style={{
            top: g.top,
            left: g.left,
            width: g.w,
            height: g.h,
            background: `linear-gradient(160deg, ${g.color}aa, ${g.color}55 50%, ${g.color}22)`,
            opacity: 0.18,
            transform: `rotate(${g.rotate}deg)`,
          }}
          animate={{
            y: [0, -12, 0],
            opacity: [0.14, 0.22, 0.14],
          }}
          transition={{
            duration: 8 + i,
            repeat: Infinity,
            ease: "easeInOut",
            delay: g.delay,
          }}
        />
      ))}
      {/* Soft inner-glow overlay so the ghosts blend into the
          aurora rather than reading as flat tiles. */}
      <div
        className="absolute inset-0 backdrop-blur-[3px]"
        style={{
          maskImage:
            "radial-gradient(circle at 50% 50%, transparent 25%, black 75%)",
          WebkitMaskImage:
            "radial-gradient(circle at 50% 50%, transparent 25%, black 75%)",
        }}
      />
    </div>
  );
}
